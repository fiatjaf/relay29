package main

import (
	"context"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/maps"
	"golang.org/x/time/rate"
)

type Group struct {
	ID      string
	Name    string
	Picture string
	About   string
	Members map[string]*Role

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
)

var availablePermissions = map[Permission]struct{}{
	PermAddUser:          {},
	PermEditMetadata:     {},
	PermDeleteEvent:      {},
	PermRemoveUser:       {},
	PermAddPermission:    {},
	PermRemovePermission: {},
}

var (
	groups = make(map[string]*Group)

	// used for the default role, the actual relay, hidden otherwise
	masterRole *Role = &Role{"master", map[Permission]struct{}{
		PermAddUser:          {},
		PermEditMetadata:     {},
		PermDeleteEvent:      {},
		PermRemoveUser:       {},
		PermAddPermission:    {},
		PermRemovePermission: {},
	}}

	// used for normal members without admin powers, not displayed
	emptyRole *Role = nil
)

// loadGroup loads all the group metadata from all the past action messages
func loadGroup(ctx context.Context, id string) *Group {
	if group, ok := groups[id]; ok {
		return group
	}

	group := &Group{
		ID: id,
		Members: map[string]*Role{
			s.RelayPubkey: masterRole,
		},

		// very strict rate limits
		bucket: rate.NewLimiter(rate.Every(time.Minute*2), 15),
	}
	ch, _ := db.QueryEvents(ctx, nostr.Filter{
		Limit: 5000, Kinds: maps.Keys(moderationActionFactories), Tags: nostr.TagMap{"h": []string{id}},
	})

	events := make([]*nostr.Event, 0, 5000)
	for event := range ch {
		events = append(events, event)
	}
	if len(events) == 0 {
		// create group here
		return group
	}
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		act, _ := moderationActionFactories[evt.Kind](evt)
		act.Apply(group)
	}

	groups[id] = group
	return group
}
