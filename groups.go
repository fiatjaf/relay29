package relay29

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

type Group struct {
	nip29.Group
	mu sync.RWMutex

	Parent         string
	ParentAttester string
	Children       map[string]struct{}
	ChildEntries   []ChildEntry
	ClosedChildren bool

	last50      []string
	last50index atomic.Int32
}

// ChildEntry is an entry of a parent group's bilateral acceptance list, carried
// as ["child", "<id>", "<order>", "<flags>...] on kind:39000.
type ChildEntry struct {
	ID    string
	Order string
	Flags []string
}

// NewGroup creates a new group from scratch (but doesn't store it in the groups map)
func (s *State) NewGroup(id string, creator string) *Group {
	group := &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: "wss://" + s.Domain,
			},
			Roles:   s.defaultRoles,
			Members: make(map[string][]*nip29.Role, 12),
		},
		Children: make(map[string]struct{}),
		last50:   make([]string, 50),
	}

	group.Members[creator] = []*nip29.Role{s.groupCreatorDefaultRole}

	return group
}

// loadGroupsFromDB loads all the group metadata from all the past action messages.
func (s *State) loadGroupsFromDB(ctx context.Context) error {
	groupMetadataEvents, err := s.DB.QueryEvents(ctx, nostr.Filter{Kinds: []int{nostr.KindSimpleGroupCreateGroup}})
	if err != nil {
		return err
	}
	for evt := range groupMetadataEvents {
		gtag := evt.Tags.GetFirst([]string{"h", ""})
		id := (*gtag)[1]

		group := s.NewGroup(id, evt.PubKey)
		f := nostr.Filter{
			Limit: 5000, Kinds: nip29.ModerationEventKinds, Tags: nostr.TagMap{"h": []string{id}},
		}
		ch, err := s.DB.QueryEvents(ctx, f)
		if err != nil {
			return err
		}

		events := make([]*nostr.Event, 0, 5000)
		for event := range ch {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, err := PrepareModerationAction(evt)
			if err != nil {
				return err
			}
			act.Apply(&group.Group)
		}

		// if the group was deleted there will be no actions after the delete
		if len(events) > 0 && events[0].Kind == nostr.KindSimpleGroupDeleteGroup {
			// we don't keep track of this if it was deleted
			continue
		}

		// load the last 50 event ids for "previous" tag checking
		i := 49
		ch, err = s.DB.QueryEvents(ctx, nostr.Filter{Tags: nostr.TagMap{"h": []string{id}}, Limit: 50})
		if err != nil {
			return err
		}
		for evt := range ch {
			group.last50[i] = evt.ID
			i--
		}

		s.Groups.Store(group.Address.ID, group)
	}

	// second pass: replay every kind:9002 in GLOBAL chronological order to
	// rebuild subgroup state (parent link, attester pubkey, child entries,
	// closed-children flag) — those fields live on the wrapper Group and are
	// therefore not touched by the first-pass Action.Apply. A single global
	// order is required so cycle-check sees the same state the runtime did
	// when it accepted each event.
	metadataEvents, err := s.DB.QueryEvents(ctx, nostr.Filter{Kinds: []int{nostr.KindSimpleGroupEditMetadata}})
	if err != nil {
		return err
	}
	allEvents := make([]*nostr.Event, 0)
	for evt := range metadataEvents {
		if evt.Tags.GetFirst([]string{"h", ""}) == nil {
			continue
		}
		allEvents = append(allEvents, evt)
	}
	sort.Slice(allEvents, func(i, j int) bool { return allEvents[i].CreatedAt < allEvents[j].CreatedAt })
	for _, evt := range allEvents {
		id := (*evt.Tags.GetFirst([]string{"h", ""}))[1]
		group, _ := s.Groups.Load(id)
		if group == nil {
			continue
		}
		if pt := evt.Tags.GetFirst([]string{"parent"}); pt != nil {
			parentId := ""
			if len(*pt) >= 2 {
				parentId = (*pt)[1]
			}
			// skip parent updates that would form a cycle against the state
			// already rebuilt — a kind:9002 rejected at runtime (e.g. the
			// loser of a concurrent-reparent race) may still be in the DB,
			// and replaying it blindly would corrupt the in-memory tree.
			// Other fields on the same event (child entries, closed-children)
			// are still applied below.
			if parentId == "" || !s.WouldCreateCycle(id, parentId) {
				// detach from previous parent
				if group.Parent != "" && group.Parent != parentId {
					if old, _ := s.Groups.Load(group.Parent); old != nil {
						delete(old.Children, id)
					}
				}
				group.Parent = parentId
				if parentId != "" {
					group.ParentAttester = evt.PubKey
					if parent, _ := s.Groups.Load(parentId); parent != nil {
						parent.Children[id] = struct{}{}
					}
				} else {
					group.ParentAttester = ""
				}
			}
		}
		if childTags := evt.Tags.GetAll([]string{"child"}); len(childTags) > 0 {
			entries := make([]ChildEntry, 0, len(childTags))
			for _, tag := range childTags {
				if len(tag) < 2 || tag[1] == "" {
					continue
				}
				entry := ChildEntry{ID: tag[1]}
				if len(tag) >= 3 {
					entry.Order = tag[2]
				}
				if len(tag) >= 4 {
					entry.Flags = append([]string(nil), tag[3:]...)
				}
				entries = append(entries, entry)
			}
			group.ChildEntries = entries
		}
		if evt.Tags.GetFirst([]string{"closed-children"}) != nil {
			group.ClosedChildren = true
		} else if evt.Tags.GetFirst([]string{"open-children"}) != nil {
			group.ClosedChildren = false
		}
	}

	return nil
}

func (s *State) GetGroupFromEvent(event *nostr.Event) *Group {
	group, _ := s.Groups.Load(GetGroupIDFromEvent(event))
	return group
}

func GetGroupIDFromEvent(event *nostr.Event) string {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	return groupId
}
