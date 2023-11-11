package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func restrictWritesBasedOnGroupRules(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	if gtag == nil {
		return true, "missing group (`h`) tag"
	}

	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)

	// only members can write
	if _, isMember := group.Members[event.PubKey]; !isMember {
		return true, "unknown member"
	}

	// if this is a moderation action, check if the user should be allowed to perform it
	if event.Kind == 9000 {
		role, ok := group.Members[event.PubKey]
		if !ok || role == emptyRole {
			return true, "not a moderator"
		}
		for _, tag := range event.Tags.GetAll([]string{"action", ""}) {
			switch tag[1] {
			case "add-user":
				if !role.AddUser || len(tag) < 3 {
					return true, "invalid action " + tag[1]
				}
			case "edit-metadata":
				if !role.EditMetadata || len(tag) != 4 {
					return true, "invalid action " + tag[1]
				}
			case "ban-user":
				if !role.BanUser || len(tag) != 3 {
					return true, "invalid action " + tag[1]
				}
			case "add-permission":
				if !role.AddPermission || len(tag) != 4 {
					return true, "invalid action " + tag[1]
				}
			case "remove-permission":
				if !role.RemovePermission || len(tag) != 4 {
					return true, "invalid action " + tag[1]
				}
			default:
				return false, "unknown action " + tag[1]
			}
		}
		if rsv := group.bucket.Reserve(); rsv.Delay() != 0 {
			rsv.Cancel()
			return true, "rate-limited"
		} else {
			rsv.OK()
		}
	}

	// write allowed
	return false, ""
}

func blockDeletesOfOldMessages(ctx context.Context, target, deletion *nostr.Event) (acceptDeletion bool, msg string) {
	if target.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
		return false, "can't delete old event, contact relay admin"
	}

	return true, ""
}

func applyModerationAction(ctx context.Context, event *nostr.Event) {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group := loadGroup(ctx, groupId)
	applyAction(group, event)
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
					// TODO
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
