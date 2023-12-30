package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"
)

type Group struct {
	ID      string
	Name    string
	Picture string
	About   string
	Members map[string]*Role
	Private bool
	Closed  bool

	bucket *rate.Limiter
}

type Role struct {
	Name        string
	Permissions map[Permission]struct{}
}

type Permission = string

const (
	PermAddUser          Permission = "add-user"
	PermEditMetadata     Permission = "edit-metadata"
	PermDeleteEvent      Permission = "delete-event"
	PermRemoveUser       Permission = "remove-user"
	PermAddPermission    Permission = "add-permission"
	PermRemovePermission Permission = "remove-permission"
	PermEditGroupStatus  Permission = "edit-group-status"
)

var availablePermissions = map[Permission]struct{}{
	PermAddUser:          {},
	PermEditMetadata:     {},
	PermDeleteEvent:      {},
	PermRemoveUser:       {},
	PermAddPermission:    {},
	PermRemovePermission: {},
	PermEditGroupStatus:  {},
}

var (
	groups     []*Group
	groupsLock sync.Mutex

	// used for the default role, the actual relay, hidden otherwise
	masterRole *Role = &Role{"master", map[Permission]struct{}{
		PermAddUser:          {},
		PermEditMetadata:     {},
		PermDeleteEvent:      {},
		PermRemoveUser:       {},
		PermAddPermission:    {},
		PermRemovePermission: {},
		PermEditGroupStatus:  {},
	}}

	// used for normal members without admin powers, not displayed
	emptyRole *Role = nil
)

func newGroup(id string) *Group {
	return &Group{
		ID: id,
		Members: map[string]*Role{
			s.RelayPubkey: masterRole,
		},

		// very strict rate limits
		bucket: rate.NewLimiter(rate.Every(time.Minute*2), 15),
	}
}

// loadGroups loads all the group metadata from all the past action messages
func loadGroups(ctx context.Context) {
	groupsLock.Lock()
	defer groupsLock.Unlock()

	groupMetadataEvents, _ := db.QueryEvents(ctx, nostr.Filter{Limit: db.MaxLimit, Kinds: maps.Keys(moderationActionFactories)})
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
			Limit: 5000, Kinds: maps.Keys(moderationActionFactories), Tags: nostr.TagMap{"h": []string{id}},
		})

		events := make([]*nostr.Event, 0, 5000)
		for event := range ch {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, _ := moderationActionFactories[evt.Kind](evt)
			act.Apply(group)
		}

		groups = append(groups, group)
	}

	slices.SortFunc(groups, func(a, b *Group) int { return strings.Compare(a.ID, b.ID) })
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

	idx, ok := slices.BinarySearchFunc(groups, group.ID, groupComparator)
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
	return strings.Compare(g.ID, id)
}
