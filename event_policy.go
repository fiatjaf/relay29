package main

import (
	"context"
	"time"

	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
)

const TOO_OLD = 60 // seconds

// events that just got deleted will be cached here for TOO_OLD seconds such that someone doesn't rebroadcast
// them -- after that time we won't accept them anymore, so we can remove their ids from this cache
var deletedCache = set.NewSliceSet[string]()

func requireHTagForExistingGroup(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	if gtag == nil {
		return true, "missing group (`h`) tag"
	}

	if group := getGroupFromEvent(event); group == nil {
		return true, "group '" + (*gtag)[1] + "' doesn't exist"
	}

	return false, ""
}

func restrictWritesBasedOnGroupRules(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	// check it only for normal write events
	if event.Kind == 9 || event.Kind == 11 {
		group := getGroupFromEvent(event)

		// only members can write
		if _, isMember := group.Members[event.PubKey]; !isMember {
			return true, "unknown member"
		}
	}

	return false, ""
}

func preventWritingOfEventsJustDeleted(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if deletedCache.Has(event.ID) {
		return true, "this was deleted"
	}
	return false, ""
}

func restrictInvalidModerationActions(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if !nip29.MetadataEventKinds.Includes(event.Kind) {
		return false, ""
	}

	// moderation action events must be new and not reused
	if event.CreatedAt < nostr.Now()-TOO_OLD {
		return true, "moderation action is too old (older than 1 minute ago)"
	}

	// will check if the moderation event author has sufficient permissions to perform this action
	// except for the relay owner/pubkey, that has infinite permissions already
	if event.PubKey == s.RelayPubkey {
		return false, ""
	}

	action, err := nip29_relay.GetModerationAction(event)
	if err != nil {
		return true, "invalid moderation action: " + err.Error()
	}

	group := getGroupFromEvent(event)
	role, ok := group.Members[event.PubKey]
	if !ok || role == nip29.EmptyRole {
		return true, "unknown admin"
	}
	if _, ok := role.Permissions[action.PermissionName()]; !ok {
		return true, "insufficient permissions"
	}
	return false, ""
}

func rateLimit(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	group := getGroupFromEvent(event)
	if rsv := group.bucket.Reserve(); rsv.Delay() != 0 {
		rsv.Cancel()
		return true, "rate-limited"
	} else {
		rsv.OK()
		return
	}
}

func applyModerationAction(ctx context.Context, event *nostr.Event) {
	action, err := nip29_relay.GetModerationAction(event)
	if err != nil {
		return
	}
	group := getGroupFromEvent(event)
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
				res, err := db.QueryEvents(ctx, nostr.Filter{IDs: []string{id}})
				if err != nil {
					log.Warn().Err(err).Msg("failed to query event to be deleted")
					continue
				}
				for target := range res {
					if err := db.DeleteEvent(ctx, target); err != nil {
						log.Warn().Err(err).Stringer("event", target).Msg("failed to delete")
					} else {
						deletedCache.Add(target.ID)
						go func(id string) {
							time.Sleep(TOO_OLD * time.Second)
							deletedCache.Remove(id)
						}(target.ID)
					}
				}
			}
		}
	}

	// propagate new replaceable events to listeners
	switch event.Kind {
	case nostr.KindSimpleGroupEditMetadata, nostr.KindSimpleGroupEditGroupStatus:
		evt := group.ToMetadataEvent()
		evt.Sign(s.RelayPrivkey)
		relay.BroadcastEvent(evt)
	case nostr.KindSimpleGroupAddPermission, nostr.KindSimpleGroupRemovePermission:
		evt := group.ToMetadataEvent()
		evt.Sign(s.RelayPrivkey)
		relay.BroadcastEvent(evt)
	case nostr.KindSimpleGroupAddUser, nostr.KindSimpleGroupRemoveUser:
		evt := group.ToMembersEvent()
		evt.Sign(s.RelayPrivkey)
		relay.BroadcastEvent(evt)
	}
}

func reactToJoinRequest(ctx context.Context, event *nostr.Event) {
	if event.Kind != 9021 {
		return
	}

	group := getGroupFromEvent(event)
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
		if err := addUser.Sign(s.RelayPrivkey); err != nil {
			log.Error().Err(err).Msg("failed to sign add-user event")
			return
		}
		if _, err := relay.AddEvent(ctx, addUser); err != nil {
			log.Error().Err(err).Msg("failed to add user who requested to join")
			return
		}
		relay.BroadcastEvent(addUser)
	}
}

func blockDeletesOfOldMessages(ctx context.Context, target, deletion *nostr.Event) (acceptDeletion bool, msg string) {
	if target.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
		return false, "can't delete old event, contact relay admin"
	}

	return true, ""
}
