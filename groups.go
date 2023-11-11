package main

import (
	"context"
	"time"

	"github.com/nbd-wtf/go-nostr"
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
	Name             string
	AddUser          bool
	EditMetadata     bool
	DeleteEvent      bool
	BanUser          bool
	AddPermission    bool
	RemovePermission bool
}

var (
	groups = make(map[string]*Group)

	// used for the default role, the actual relay, hidden otherwise
	masterRole *Role = &Role{"master", true, true, true, true, true, true}

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
	ch, _ := db.QueryEvents(ctx, nostr.Filter{Limit: 5000, Kinds: []int{9000}, Tags: nostr.TagMap{"h": []string{id}}})

	events := make([]*nostr.Event, 0, 5000)
	for event := range ch {
		events = append(events, event)
	}
	if len(events) == 0 {
		// create group here
		return group
	}
	for i := len(events) - 1; i >= 0; i-- {
		applyAction(group, events[i])
	}

	groups[id] = group
	return group
}

func applyAction(group *Group, action *nostr.Event) {
	for _, tag := range action.Tags.GetAll([]string{"action", ""}) {
		switch tag[1] {
		case "add-user":
			for _, id := range tag[2:] {
				group.Members[id] = emptyRole
			}
		case "edit-metadata":
			switch tag[2] {
			case "name":
				group.Name = tag[3]
			case "picture":
				group.Picture = tag[3]
			case "about":
				group.About = tag[3]
			}
		case "ban-user":
			delete(group.Members, tag[2])
		case "add-permission":
			role, ok := group.Members[tag[2]]
			if !ok || role == nil {
				role = &Role{}
				group.Members[tag[2]] = role
			}
			switch tag[3] {
			case "add-user":
				role.AddUser = true
			case "edit-metadata":
				role.EditMetadata = true
			case "delete-event":
				role.DeleteEvent = true
			case "ban-user":
				role.BanUser = true
			case "add-permission":
				role.AddPermission = true
			case "remove-permission":
				role.RemovePermission = true
			}
		case "remove-permission":
			if role, ok := group.Members[tag[2]]; ok && role != nil {
				switch tag[3] {
				case "add-user":
					role.AddUser = false
				case "edit-metadata":
					role.EditMetadata = false
				case "delete-event":
					role.DeleteEvent = false
				case "ban-user":
					role.BanUser = false
				case "add-permission":
					role.AddPermission = false
				case "remove-permission":
					role.RemovePermission = false
				}
				if !role.AddPermission && !role.RemovePermission && !role.BanUser && !role.DeleteEvent && !role.EditMetadata && !role.AddUser && role.Name == "" {
					group.Members[tag[2]] = emptyRole
				}
			}
		}
	}
}
