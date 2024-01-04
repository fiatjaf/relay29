package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"golang.org/x/exp/slices"
)

func metadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, 39000) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range groups {
					if group.Private {
						continue
					}
					if group.Closed {
						continue
					}
					evt := group.ToMetadataEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group := getGroup(groupId); group != nil {
						evt := group.ToMetadataEvent()
						evt.Sign(s.RelayPrivkey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func adminsQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)
	if slices.Contains(filter.Kinds, 39001) {
		for _, groupId := range filter.Tags["d"] {
			group := getGroup(groupId)
			if group == nil {
				continue
			}

			evt := &nostr.Event{
				Kind:      39001,
				CreatedAt: nostr.Now(),
				Content:   "list of admins for group " + groupId,
				Tags: nostr.Tags{
					nostr.Tag{"d", group.ID},
				},
			}
			for pubkey, role := range group.Members {
				if role != nip29.EmptyRole {
					tag := nostr.Tag{pubkey, "admin"}
					for permName := range role.Permissions {
						tag = append(tag, string(permName))
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
