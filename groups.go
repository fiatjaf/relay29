package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"
)

type Group struct {
	nip29.Group
	bucket *rate.Limiter
}

var (
	groups     []*Group
	groupsLock sync.Mutex
)

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
	groupsLock.Lock()
	defer groupsLock.Unlock()

	groupMetadataEvents, _ := db.QueryEvents(ctx, nostr.Filter{Limit: db.MaxLimit, Kinds: nip29.ModerationEventKinds})
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

		groups = append(groups, group)
	}

	slices.SortFunc(groups, func(a, b *Group) int { return strings.Compare(a.Address.ID, b.Address.ID) })
}

func getGroup(id string) *Group {
	groupsLock.Lock()
	defer groupsLock.Unlock()

	idx, ok := slices.BinarySearchFunc(groups, id, groupComparator)
	if !ok {
		return nil
	}
	return groups[idx]
}

func addGroup(group *Group) error {
	groupsLock.Lock()
	defer groupsLock.Unlock()

	idx, ok := slices.BinarySearchFunc(groups, group.Address.ID, groupComparator)
	if ok {
		return fmt.Errorf("a group with this id already exists")
	}

	groups = append(groups[0:idx], nil) // bogus
	copy(groups[idx+1:], groups[idx:])
	groups[idx] = group

	return nil
}

func getGroupFromEvent(event *nostr.Event) *Group {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	return getGroup(groupId)
}

func groupComparator(g *Group, id string) int {
	return strings.Compare(g.Address.ID, id)
}
