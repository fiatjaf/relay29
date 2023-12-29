package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func metadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	makeEvent39000 := func(group *Group) *nostr.Event {
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

		// status
		if group.Private {
			evt.Tags = append(evt.Tags, nostr.Tag{"private"})
		} else {
			evt.Tags = append(evt.Tags, nostr.Tag{"public"})
		}
		if group.Closed {
			evt.Tags = append(evt.Tags, nostr.Tag{"closed"})
		} else {
			evt.Tags = append(evt.Tags, nostr.Tag{"open"})
		}

		// sign
		evt.Sign(s.RelayPrivkey)
		return evt
	}

	ch := make(chan *nostr.Event, 1)
	if slices.Contains(filter.Kinds, 39000) {
		go func() {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				groupMetadataEvents, _ := db.QueryEvents(ctx, nostr.Filter{Limit: db.MaxLimit, Kinds: []int{9002}})
				for evt := range groupMetadataEvents {
					ch <- makeEvent39000(loadGroup(ctx, evt.Tags.GetD()))
				}
			}

			for _, groupId := range filter.Tags["d"] {
				ch <- makeEvent39000(loadGroup(ctx, groupId))
			}
		}()
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
