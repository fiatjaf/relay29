package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

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

func restrictInvalidModerationActions(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if event.Kind < 9000 || event.Kind > 9020 {
		return false, ""
	}
	makeModerationAction, ok := moderationActionFactories[event.Kind]
	if !ok {
		return true, "unknown moderation action"
	}
	action, err := makeModerationAction(event)
	if err != nil {
		return true, "invalid moderation action: " + err.Error()
	}

	group := getGroupFromEvent(event)
	role, ok := group.Members[event.PubKey]
	if !ok || role == emptyRole {
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
	if event.Kind < 9000 || event.Kind > 9020 {
		return
	}
	makeModerationAction, ok := moderationActionFactories[event.Kind]
	if !ok {
		return
	}
	action, err := makeModerationAction(event)
	if err != nil {
		return
	}
	group := getGroupFromEvent(event)
	action.Apply(group)

	if event.Kind == nostr.KindSimpleGroupEditMetadata || event.Kind == nostr.KindSimpleGroupEditGroupStatus {
		evt := group.ToMetadataEvent()
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
		// immediatelly add the requester
		addUser := &nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      9000,
			Tags: nostr.Tags{
				nostr.Tag{"p", event.PubKey},
			},
		}
		if err := addUser.Sign(s.RelayPrivkey); err != nil {
			log.Error().Err(err).Msg("failed to sign add-user event")
			return
		}
		if err := relay.AddEvent(ctx, addUser); err != nil {
			log.Error().Err(err).Msg("failed to add user who requested to join")
			return
		}
	}
}

func blockDeletesOfOldMessages(ctx context.Context, target, deletion *nostr.Event) (acceptDeletion bool, msg string) {
	if target.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
		return false, "can't delete old event, contact relay admin"
	}

	return true, ""
}
