package relay29

import (
	"context"
	"fmt"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/rs/zerolog/log"
)

const tooOld = 60 // seconds

func (s *State) RequireHTagForExistingGroup(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
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

func (s *State) RestrictWritesBasedOnGroupRules(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	group := s.GetGroupFromEvent(event)

	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		group.mu.RLock()
		defer group.mu.RUnlock()

		if _, isMemberAlready := group.Members[event.PubKey]; isMemberAlready {
			// unless you're already a member
			return true, "duplicate: already a member"
		}

		// Check if group is closed and validate invite code
		if group.Closed {
			codeTag := event.Tags.GetFirst([]string{"code", ""})
			if codeTag == nil {
				return true, "group is closed, invite code required"
			}

			code := (*codeTag)[1]
			inviteCode, exists := s.GetValidInviteCode(code)
			if !exists || inviteCode.GroupID != group.Address.ID {
				return true, "invalid invite code"
			}
		}

		return false, ""
	}

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		// anyone can create new groups (if this is not desired a policy must be added to filter out this stuff)
		if group == nil {
			// well, as long as the group doesn't exist, of course
			return false, ""
		} else {
			return true, "duplicate: group already exists"
		}
	}

	// the relay can write to any group
	if event.PubKey == s.publicKey {
		return false, ""
	}

	// aside from that only members can write
	group.mu.RLock()
	defer group.mu.RUnlock()
	if _, isMember := group.Members[event.PubKey]; !isMember {
		return true, "unknown member"
	}

	return false, ""
}

func (s *State) PreventWritingOfEventsJustDeleted(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if s.deletedCache.Has(event.ID) {
		return true, "this was deleted"
	}
	return false, ""
}

func (s *State) RequireModerationEventsToBeRecent(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	// moderation action events must be new and not reused
	if nip29.ModerationEventKinds.Includes(event.Kind) && event.CreatedAt < nostr.Now()-tooOld {
		return true, "moderation action is too old (older than 1 minute ago)"
	}
	return false, ""
}

func (s *State) RestrictInvalidModerationActions(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if !nip29.ModerationEventKinds.Includes(event.Kind) {
		return false, ""
	}

	group := s.GetGroupFromEvent(event)
	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		// see restrictWritesBasedOnGroupRules for a check that a group cannot be created if it already exists
		return false, ""
	}

	// will check if the moderation event author has sufficient permissions to perform this action
	// except for the relay owner/pubkey, that has infinite permissions already
	if event.PubKey == s.publicKey {
		return false, ""
	}

	action, err := PrepareModerationAction(event)
	if err != nil {
		return true, "invalid moderation action: " + err.Error()
	}

	if egs, ok := action.(EditMetadata); ok && egs.PrivateValue != nil && *egs.PrivateValue && !s.AllowPrivateGroups {
		return true, "groups cannot be private"
	}

	group.mu.RLock()
	defer group.mu.RUnlock()
	roles, _ := group.Members[event.PubKey]

	// create-group doesn't trigger an AllowAction call -- it must be prevented on khatru's RejectEvent hook
	if _, ok := action.(CreateGroup); !ok {
		if s.AllowAction != nil {
			for _, role := range roles {
				if s.AllowAction(ctx, group.Group, role, action) {
					// if any roles allow it, we are good
					return false, ""
				}
			}
		}
	}

	// otherwise everything is forbidden (by default everything is forbidden)
	return true, "insufficient permissions"
}

func (s *State) CheckPreviousTag(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	previous := event.Tags.GetFirst([]string{"previous"})
	if previous == nil {
		return false, ""
	}

	group := s.GetGroupFromEvent(event)
	for _, idFirstChars := range (*previous)[1:] {
		if len(idFirstChars) > 64 {
			return true, fmt.Sprintf("invalid value '%s' in previous tag", idFirstChars)
		}
		found := false
		for _, id := range group.last50 {
			if id == "" {
				continue
			}
			if id[0:len(idFirstChars)] == idFirstChars {
				found = true
				break
			}
		}
		if !found {
			return true, fmt.Sprintf("previous id '%s' wasn't found in this relay", idFirstChars)
		}
	}

	return false, ""
}

func (s *State) AddToPreviousChecking(ctx context.Context, event *nostr.Event) {
	group := s.GetGroupFromEvent(event)
	if group == nil {
		return
	}
	lastIndex := group.last50index.Add(1) - 1
	group.last50[lastIndex%50] = event.ID
}

func (s *State) ApplyModerationAction(ctx context.Context, event *nostr.Event) {
	// turn event into a moderation action processor
	action, err := PrepareModerationAction(event)
	if err != nil {
		return
	}

	// get group (or create it)
	var group *Group
	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		// if it's a group creation event we create the group first
		groupId := GetGroupIDFromEvent(event)
		group = s.NewGroup(groupId, event.PubKey)
		group.Roles = s.defaultRoles
		s.Groups.Store(groupId, group)
	} else {
		group = s.GetGroupFromEvent(event)
	}

	// apply the moderation action
	group.mu.Lock()
	action.Apply(&group.Group, s)
	group.mu.Unlock()

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
	} else if event.Kind == nostr.KindSimpleGroupDeleteGroup {
		// when the group was deleted we just remove it
		s.Groups.Delete(group.Address.ID)
	}

	// propagate new replaceable events to listeners depending on what changed happened
	for _, toBroadcast := range map[int][]func() *nostr.Event{
		nostr.KindSimpleGroupCreateGroup: {
			group.ToMetadataEvent,
			group.ToAdminsEvent,
			group.ToMembersEvent,
			group.ToRolesEvent,
		},
		nostr.KindSimpleGroupEditMetadata: {
			group.ToMetadataEvent,
		},
		nostr.KindSimpleGroupPutUser: {
			group.ToMembersEvent,
			group.ToAdminsEvent,
		},
		nostr.KindSimpleGroupRemoveUser: {
			group.ToMembersEvent,
		},
	}[event.Kind] {
		evt := toBroadcast()
		evt.Sign(s.secretKey)
		s.Relay.BroadcastEvent(evt)
	}
}

func (s *State) ReactToJoinRequest(ctx context.Context, event *nostr.Event) {
	if event.Kind != nostr.KindSimpleGroupJoinRequest {
		return
	}

	group := s.GetGroupFromEvent(event)
	if group == nil {
		return
	}

	// Check if user was previously removed
	ch, err := s.DB.QueryEvents(ctx, nostr.Filter{
		Kinds: []int{nostr.KindSimpleGroupRemoveUser},
		Tags: nostr.TagMap{
			"p": []string{event.PubKey},
			"h": []string{group.Address.ID},
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("failed to check if requested user was previously removed")
		return
	}

	// this means the user was previously removed
	if nil != <-ch {
		log.Error().Str("pubkey", event.PubKey).Msg("denying access to previously removed user")
		return
	}

	// Add the user to the group
	addUser := &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{
			nostr.Tag{"h", group.Address.ID},
			nostr.Tag{"p", event.PubKey},
		},
	}
	if err := addUser.Sign(s.secretKey); err != nil {
		log.Error().Err(err).Msg("failed to sign add-user event")
		return
	}
	if _, err := s.Relay.AddEvent(ctx, addUser); err != nil {
		log.Error().Err(err).Msg("failed to add user who requested to join")
		return
	}
	s.Relay.BroadcastEvent(addUser)
}

func (s *State) ReactToLeaveRequest(ctx context.Context, event *nostr.Event) {
	if event.Kind != nostr.KindSimpleGroupLeaveRequest {
		return
	}

	group := s.GetGroupFromEvent(event)

	if _, isMember := group.Members[event.PubKey]; isMember {
		// immediately remove the requester
		removeUser := &nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupRemoveUser,
			Tags: nostr.Tags{
				nostr.Tag{"h", group.Address.ID},
				nostr.Tag{"p", event.PubKey},
			},
		}
		if err := removeUser.Sign(s.secretKey); err != nil {
			log.Error().Err(err).Msg("failed to sign remove-user event")
			return
		}
		if _, err := s.Relay.AddEvent(ctx, removeUser); err != nil {
			log.Error().Err(err).Msg("failed to remove user who requested to leave")
			return
		}
		s.Relay.BroadcastEvent(removeUser)
	}
}
