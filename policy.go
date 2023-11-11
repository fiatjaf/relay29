package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
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

func blockDeletesOfOldMessages(ctx context.Context, target, deletion *nostr.Event) (acceptDeletion bool, msg string) {
	if target.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
		return false, "can't delete old event, contact relay admin"
	}

	return true, ""
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

func metadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)
	if slices.Contains(filter.Kinds, 39000) {
		for _, groupId := range filter.Tags["d"] {
			group := loadGroup(ctx, groupId)
			evt := &nostr.Event{
				Kind:      39000,
				CreatedAt: nostr.Now(),
				Content:   group.About,
				Tags: nostr.Tags{
					nostr.Tag{"d", group.ID},
				},
			}
			if group.Name != "" {
				evt.Tags = append(evt.Tags, nostr.Tag{"name", group.Name})
			}
			if group.Picture != "" {
				evt.Tags = append(evt.Tags, nostr.Tag{"picture", group.Picture})
			}
			evt.Sign(s.RelayPrivkey)
			ch <- evt
		}
	}
	close(ch)
	return ch, nil
}

func adminsQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)
	if slices.Contains(filter.Kinds, 39001) {
		for _, groupId := range filter.Tags["d"] {
			group := loadGroup(ctx, groupId)
			evt := &nostr.Event{
				Kind:      39001,
				CreatedAt: nostr.Now(),
				Content:   "list of admins for group " + groupId,
				Tags: nostr.Tags{
					nostr.Tag{"d", group.ID},
				},
			}
			for pubkey, role := range group.Members {
				if role != emptyRole && role != masterRole {
					tag := nostr.Tag{pubkey, "admin"}
					for permName := range role.Permissions {
						tag = append(tag, permName)
					}
					evt.Tags = append(evt.Tags, tag)
				}
			}
			evt.Sign(s.RelayPrivkey)
			ch <- evt
		}
	}
	close(ch)
	return ch, nil
}

func requireKindAndSingleGroupID(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	if len(filter.Kinds) == 0 {
		return true, "must specify kinds"
	}

	isMeta := false
	isNormal := false
	for _, kind := range filter.Kinds {
		if kind < 10000 {
			isNormal = true
		} else if kind >= 30000 {
			isMeta = true
		}
	}
	if isNormal && isMeta {
		return true, "cannot request both meta and normal events at the same time"
	}
	if !isNormal && !isMeta {
		return true, "unexpected kinds requested"
	}

	if isNormal {
		if ids, ok := filter.Tags["h"]; ok && len(ids) != 0 {
			return true, "must have an 'h' tag"
		}
	}

	return false, ""
}
