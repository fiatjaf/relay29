package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

func requireHTag(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	if gtag == nil {
		return true, "missing group (`h`) tag"
	}
	return false, ""
}

func restrictWritesBasedOnGroupRules(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	// check it only for normal write events
	if event.Kind == 9 || event.Kind == 11 {
		gtag := event.Tags.GetFirst([]string{"h", ""})
		groupId := (*gtag)[1]
		group := loadGroup(ctx, groupId)

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
	action, ok := makeModerationAction(event)
	if !ok {
		return true, "invalid moderation action"
	}

	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)

	role, ok := group.Members[event.PubKey]
	if !ok || role == emptyRole {
		return true, "unknown admin"
	}
	if _, ok := role.Permissions[action.PermissionRequired()]; !ok {
		return true, "insufficient permissions"
	}
	return false, ""
}

func rateLimit(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)

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
	action, ok := makeModerationAction(event)
	if !ok {
		return
	}
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)
	action.Apply(group)
}

func reactToJoinRequest(ctx context.Context, event *nostr.Event) {
	if event.Kind != 9021 {
		return
	}
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)

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
