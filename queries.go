package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

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
