package relay29

import (
	"github.com/nbd-wtf/go-nostr"
)

// ToMetadataEvent shadows the embedded nip29.Group.ToMetadataEvent so the
// kind:39000 carries the subgroup-related tags: `parent` (with optional third
// element = admin attester pubkey), `child` entries, and `closed-children`.
func (g *Group) ToMetadataEvent() *nostr.Event {
	g.mu.RLock()
	defer g.mu.RUnlock()
	evt := g.Group.ToMetadataEvent()
	if g.Parent != "" {
		tag := nostr.Tag{"parent", g.Parent}
		if g.ParentAttester != "" {
			tag = append(tag, g.ParentAttester)
		}
		evt.Tags = append(evt.Tags, tag)
	}
	for _, c := range g.ChildEntries {
		tag := nostr.Tag{"child", c.ID}
		if c.Order != "" || len(c.Flags) > 0 {
			tag = append(tag, c.Order)
		}
		if len(c.Flags) > 0 {
			tag = append(tag, c.Flags...)
		}
		evt.Tags = append(evt.Tags, tag)
	}
	if g.ClosedChildren {
		evt.Tags = append(evt.Tags, nostr.Tag{"closed-children"})
	}
	return evt
}

// WouldCreateCycle reports whether making `childId` a subgroup of `parentId`
// would form a cycle. Walks the parent chain upward from parentId; if it
// reaches childId, it's a cycle.
func (s *State) WouldCreateCycle(childId, parentId string) bool {
	if childId == parentId {
		return true
	}
	visited := make(map[string]struct{})
	current := parentId
	for current != "" {
		if _, seen := visited[current]; seen {
			return true
		}
		visited[current] = struct{}{}
		if current == childId {
			return true
		}
		g, _ := s.Groups.Load(current)
		if g == nil {
			return false
		}
		current = g.Parent
	}
	return false
}

// Reparent updates the parent/children relationship of a group.
// newParent == "" means detach to root (attester is cleared too). attester is
// the pubkey of the admin whose kind:9002 authored the relationship; the relay
// copies it into the emitted kind:39000 as the third element of the `parent`
// tag (spec: Parent consent / Admin attestation).
//
// Locking: the caller MUST NOT hold group.mu. Reparent takes s.reparentMu for
// the duration, then acquires each involved group's mu in isolation (never
// nested), which rules out cross-parent deadlocks.
func (s *State) Reparent(group *Group, newParent, attester string) (oldParent string, changed bool) {
	s.reparentMu.Lock()
	defer s.reparentMu.Unlock()

	if newParent != "" && s.WouldCreateCycle(group.Address.ID, newParent) {
		return group.Parent, false
	}

	group.mu.Lock()
	oldParent = group.Parent
	oldAttester := group.ParentAttester
	if oldParent == newParent && (newParent == "" || oldAttester == attester) {
		group.mu.Unlock()
		return oldParent, false
	}
	group.Parent = newParent
	if newParent == "" {
		group.ParentAttester = ""
	} else {
		group.ParentAttester = attester
	}
	groupId := group.Address.ID
	group.mu.Unlock()

	if oldParent != "" && oldParent != newParent {
		if old, _ := s.Groups.Load(oldParent); old != nil {
			old.mu.Lock()
			delete(old.Children, groupId)
			old.mu.Unlock()
		}
	}
	if newParent != "" && oldParent != newParent {
		if np, _ := s.Groups.Load(newParent); np != nil {
			np.mu.Lock()
			np.Children[groupId] = struct{}{}
			np.mu.Unlock()
		}
	}
	return oldParent, true
}

// promoteChildrenToRoots clears the Parent link on every direct child of
// `group`, broadcasts an updated kind:39000 for each (now without a parent
// tag), and returns them. Used when `group` itself is about to be deleted — per
// spec, "when a parent is deleted, its remaining children automatically become
// roots."
func (s *State) promoteChildrenToRoots(group *Group) []*Group {
	group.mu.RLock()
	childIds := make([]string, 0, len(group.Children))
	for id := range group.Children {
		childIds = append(childIds, id)
	}
	group.mu.RUnlock()

	promoted := make([]*Group, 0, len(childIds))
	for _, id := range childIds {
		child, _ := s.Groups.Load(id)
		if child == nil {
			continue
		}
		child.mu.Lock()
		child.Parent = ""
		child.ParentAttester = ""
		child.mu.Unlock()
		promoted = append(promoted, child)
	}
	return promoted
}
