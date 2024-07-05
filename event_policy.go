package relay29

import (
	"context"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
	"github.com/rs/zerolog/log"
)

const tooOld = 60 // seconds

func (s *State) requireHTagForExistingGroup(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	// this allows us to never check again in any of the other rules if the tag exists and just assume it exists always
	gtag := event.Tags.GetFirst([]string{"h", ""})
	if gtag == nil {
		return true, "missing group (`h`) tag"
	}

	// skip this check when creating a group
	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		return false, ""
	}

	// otherwise require a group to exist always
	if group := s.GetGroupFromEvent(event); group == nil {
		return true, "group '" + (*gtag)[1] + "' doesn't exist"
	}

	return false, ""
}

func (s *State) restrictWritesBasedOnGroupRules(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	group := s.GetGroupFromEvent(event)

	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		// anyone can apply to enter any group (if this is not desired a policy must be added to filter out this stuff)
		group.mu.RLock()
		defer group.mu.RUnlock()
		if _, isMemberAlready := group.Members[event.PubKey]; isMemberAlready {
			// unless you're already a member
			return true, "already a member"
		}
		return false, ""
	}

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		// anyone can create new groups (if this is not desired a policy must be added to filter out this stuff)
		return false, ""
	}

	// only members can write
	group.mu.RLock()
	defer group.mu.RUnlock()
	if _, isMember := group.Members[event.PubKey]; !isMember {
		return true, "unknown member"
	}

	return false, ""
}

func (s *State) preventWritingOfEventsJustDeleted(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if s.deletedCache.Has(event.ID) {
		return true, "this was deleted"
	}
	return false, ""
}

func (s *State) restrictInvalidModerationActions(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if !nip29.MetadataEventKinds.Includes(event.Kind) {
		return false, ""
	}

	// moderation action events must be new and not reused
	if event.CreatedAt < nostr.Now()-tooOld {
		return true, "moderation action is too old (older than 1 minute ago)"
	}

	group := s.GetGroupFromEvent(event)
	if event.Kind == nostr.KindSimpleGroupCreateGroup && group != nil {
		return true, "group already exists"
	}

	// will check if the moderation event author has sufficient permissions to perform this action
	// except for the relay owner/pubkey, that has infinite permissions already
	if event.PubKey == s.publicKey {
		return false, ""
	}

	action, err := nip29_relay.GetModerationAction(event)
	if err != nil {
		return true, "invalid moderation action: " + err.Error()
	}

	group.mu.RLock()
	defer group.mu.RUnlock()
	role, ok := group.Members[event.PubKey]
	if !ok || role == nip29.EmptyRole {
		return true, "unknown admin"
	}
	if _, ok := role.Permissions[action.PermissionName()]; !ok {
		return true, "insufficient permissions"
	}
	return false, ""
}

func (s *State) applyModerationAction(ctx context.Context, event *nostr.Event) {
	// turn event into a moderation action processor
	action, err := nip29_relay.GetModerationAction(event)
	if err != nil {
		return
	}

	// get group (or create it)
	var group *Group
	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		// if it's a group creation event we create the group first
		groupId := GetGroupIDFromEvent(event)
		group = s.NewGroup(groupId)
		s.Groups.Store(groupId, group)
	} else {
		group = s.GetGroupFromEvent(event)
	}
	// apply the moderation action
	action.Apply(&group.Group)

	// if it's a delete event we have to actually delete stuff from the database here
	if event.Kind == nostr.KindSimpleGroupDeleteEvent {
		for _, tag := range event.Tags {
			if tag.Key() == "e" {
				id := tag.Value()
				if !nostr.IsValid32ByteHex(id) {
					log.Warn().Stringer("event", event).Msg("delete request came with a broken \"e\" tag")
					continue
				}
				res, err := s.DB.QueryEvents(ctx, nostr.Filter{IDs: []string{id}})
				if err != nil {
					log.Warn().Err(err).Msg("failed to query event to be deleted")
					continue
				}
				for target := range res {
					if err := s.DB.DeleteEvent(ctx, target); err != nil {
						log.Warn().Err(err).Stringer("event", target).Msg("failed to delete")
					} else {
						s.deletedCache.Add(target.ID)
						go func(id string) {
							time.Sleep(tooOld * time.Second)
							s.deletedCache.Remove(id)
						}(target.ID)
					}
				}
			}
		}
	}

	// propagate new replaceable events to listeners depending on what changed happened
	switch event.Kind {
	case nostr.KindSimpleGroupCreateGroup, nostr.KindSimpleGroupEditMetadata, nostr.KindSimpleGroupEditGroupStatus:
		evt := group.ToMetadataEvent()
		evt.Sign(s.privateKey)
		s.Relay.BroadcastEvent(evt)
	case nostr.KindSimpleGroupAddPermission, nostr.KindSimpleGroupRemovePermission:
		evt := group.ToMetadataEvent()
		evt.Sign(s.privateKey)
		s.Relay.BroadcastEvent(evt)
	case nostr.KindSimpleGroupAddUser, nostr.KindSimpleGroupRemoveUser:
		evt := group.ToMembersEvent()
		evt.Sign(s.privateKey)
		s.Relay.BroadcastEvent(evt)
	}
}

func (s *State) reactToJoinRequest(ctx context.Context, event *nostr.Event) {
	if event.Kind != nostr.KindSimpleGroupJoinRequest {
		return
	}

	group := s.GetGroupFromEvent(event)
	if !group.Closed {
		// immediately add the requester
		addUser := &nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupAddUser,
			Tags: nostr.Tags{
				nostr.Tag{"h", group.Address.ID},
				nostr.Tag{"p", event.PubKey},
			},
		}
		if err := addUser.Sign(s.privateKey); err != nil {
			log.Error().Err(err).Msg("failed to sign add-user event")
			return
		}
		if _, err := s.Relay.AddEvent(ctx, addUser); err != nil {
			log.Error().Err(err).Msg("failed to add user who requested to join")
			return
		}
		s.Relay.BroadcastEvent(addUser)
	}
}
