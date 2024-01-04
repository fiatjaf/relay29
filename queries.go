package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func metadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMetadata) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range groups {
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

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupAdmins) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range groups {
					evt := group.ToAdminsEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group := getGroup(groupId); group != nil {
						evt := group.ToAdminsEvent()
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

func membersQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMembers) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range groups {
					evt := group.ToMembersEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group := getGroup(groupId); group != nil {
						evt := group.ToMembersEvent()
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
