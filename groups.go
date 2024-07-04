package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
	"github.com/puzpuzpuz/xsync/v3"
	"golang.org/x/time/rate"
)

type Group struct {
	nip29.Group
	bucket *rate.Limiter
	mu     sync.RWMutex
}

var groups = xsync.NewMapOf[string, *Group]()

func newGroup(id string) *Group {
	return &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: "wss://" + s.Domain,
			},
			Members: map[string]*nip29.Role{},
		},

		// very strict rate limits
		bucket: rate.NewLimiter(rate.Every(time.Minute*2), 15),
	}
}

// loadGroups loads all the group metadata from all the past action messages
func loadGroups(ctx context.Context) {
	groupMetadataEvents, _ := db.QueryEvents(ctx, nostr.Filter{Limit: db.MaxLimit, Kinds: []int{nostr.KindSimpleGroupCreateGroup}})
	alreadySeen := set.NewSliceSet[string]()
	for evt := range groupMetadataEvents {
		gtag := evt.Tags.GetFirst([]string{"h", ""})
		id := (*gtag)[1]

		if alreadySeen.Has(id) {
			continue
		}
		alreadySeen.Add(id)

		group := newGroup(id)
		ch, _ := db.QueryEvents(ctx, nostr.Filter{
			Limit: 5000, Kinds: nip29.ModerationEventKinds, Tags: nostr.TagMap{"h": []string{id}},
		})

		events := make([]*nostr.Event, 0, 5000)
		for event := range ch {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, _ := nip29_relay.GetModerationAction(evt)
			act.Apply(&group.Group)
		}

		groups.Store(group.Address.ID, group)
	}
}

func getGroupFromEvent(event *nostr.Event) *Group {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	group, _ := groups.Load(groupId)
	return group
}

func groupComparator(g *Group, id string) int {
	return strings.Compare(g.Address.ID, id)
}
