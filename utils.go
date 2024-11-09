package relay29

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bep/debounce"
	"github.com/nbd-wtf/go-nostr"
)

var internalCallContextKey = struct{}{}

func IsInternalCall(ctx context.Context) bool {
	return ctx.Value(internalCallContextKey) != nil
}

var (
	applyEventsSerial              = atomic.Int64{}
	applyEventsSerialResetDebounce = debounce.New(time.Second * 15)
)

func (s *State) applyEvents(ctx context.Context, events ...*nostr.Event) error {
	ourCtx := context.WithValue(ctx, internalCallContextKey, struct{}{})

	for _, evt := range events {
		// this is to ensure correct ordering of events when a lot of actions are called simultaneously
		evt.CreatedAt += nostr.Timestamp(applyEventsSerial.Add(1))

		if err := evt.Sign(s.secretKey); err != nil {
			return fmt.Errorf("failed to sign event %s: %w", evt, err)
		}
		if _, err := s.Relay.AddEvent(ourCtx, evt); err != nil {
			return fmt.Errorf("failed to apply event %s: %w", evt, err)
		}
	}

	applyEventsSerialResetDebounce(func() { applyEventsSerial.Store(0) })

	return nil
}

func (s *State) CreateGroup(ctx context.Context, groupId string, defs EditMetadata) error {
	group, _ := s.Groups.Load(groupId)
	if group != nil {
		return fmt.Errorf("group '%s' already exists", groupId)
	}

	metadataTags := make([]nostr.Tag, 1, 7)

	metadataTags[0] = nostr.Tag{"h", groupId}
	if defs.NameValue != nil {
		metadataTags = append(metadataTags, nostr.Tag{"name", *defs.NameValue})
	}
	if defs.AboutValue != nil {
		metadataTags = append(metadataTags, nostr.Tag{"about", *defs.AboutValue})
	}
	if defs.PictureValue != nil {
		metadataTags = append(metadataTags, nostr.Tag{"picture", *defs.PictureValue})
	}
	if defs.ClosedValue != nil {
		if *defs.ClosedValue {
			metadataTags = append(metadataTags, nostr.Tag{"closed"})
		} else {
			metadataTags = append(metadataTags, nostr.Tag{"open"})
		}
	}
	if defs.PrivateValue != nil {
		if *defs.PrivateValue {
			metadataTags = append(metadataTags, nostr.Tag{"private"})
		} else {
			metadataTags = append(metadataTags, nostr.Tag{"public"})
		}
	}

	return s.applyEvents(ctx,
		&nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupCreateGroup,
			Tags: nostr.Tags{
				nostr.Tag{"h", groupId},
			},
		},
		&nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupEditMetadata,
			Tags:      metadataTags,
		},
	)
}

func (s *State) PutUser(ctx context.Context, groupId string, pubkey string, roles ...string) error {
	userTag := nostr.Tag{"p", pubkey}
	userTag = append(userTag, roles...)

	return s.applyEvents(ctx, &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindSimpleGroupPutUser,
		Tags: nostr.Tags{
			nostr.Tag{"h", groupId},
			userTag,
		},
	})
}

func (s *State) RemoveUserFromGroup(ctx context.Context, groupId string, pubkey string) error {
	return s.applyEvents(ctx, &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindSimpleGroupRemoveUser,
		Tags: nostr.Tags{
			nostr.Tag{"h", groupId},
			nostr.Tag{"p", pubkey},
		},
	})
}

func (s *State) DeleteEvent(ctx context.Context, groupId string, eventId string) error {
	return s.applyEvents(ctx, &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindSimpleGroupDeleteEvent,
		Tags: nostr.Tags{
			nostr.Tag{"h", groupId},
			nostr.Tag{"e", eventId},
		},
	})
}
