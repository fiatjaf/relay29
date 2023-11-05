package main

import (
	"context"

	"github.com/juju/ratelimit"
	"github.com/nbd-wtf/go-nostr"
)

type Group struct {
	ID      string
	Name    string
	Picture string
	About   string
	Members map[string]struct{}
	Admins  map[string]*Role

	bucket *ratelimit.Bucket
}

type Role struct {
	ID               string
	Name             string
	AddUser          bool
	EditMetadata     bool
	DeleteEvent      bool
	BanUser          bool
	AddPermission    bool
	RemovePermission bool
}

var groups = make(map[string]*Group)

// loadGroup loads all the group metadata from all the past action messages
func loadGroup(ctx context.Context, id string) (*Group, bool) {
	if group, ok := groups[id]; ok {
		return group, true
	}

	group := &Group{ID: id}
	ch, _ := db.QueryEvents(ctx, nostr.Filter{Limit: 5000, Kinds: []int{9000}, Tags: nostr.TagMap{"h": []string{id}}})

	events := make([]*nostr.Event, 0, 5000)
	for event := range ch {
		events = append(events, event)
	}
	if len(events) == 0 {
		return group, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		applyAction(group, events[i])
	}

	group.bucket = ratelimit.NewBucketWithRate(1/(60*5), 3) // very strict rate limits
	return group, true
}

func applyAction(group *Group, action *nostr.Event) {
	for _, tag := range action.Tags.GetAll([]string{"action", ""}) {
		switch tag[1] {
		case "add-user":
			for _, id := range tag[2:] {
				group.Members[id] = struct{}{}
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
			admin, ok := group.Admins[tag[2]]
			if !ok {
				admin = &Role{}
				group.Admins[tag[2]] = admin
			}
			switch tag[3] {
			case "add-user":
				admin.AddUser = true
			case "edit-metadata":
				admin.EditMetadata = true
			case "delete-event":
				admin.DeleteEvent = true
			case "ban-user":
				admin.BanUser = true
			case "add-permission":
				admin.AddPermission = true
			case "remove-permission":
				admin.RemovePermission = true
			}
		case "remove-permission":
			if admin, ok := group.Admins[tag[2]]; ok {
				switch tag[3] {
				case "add-user":
					admin.AddUser = false
				case "edit-metadata":
					admin.EditMetadata = false
				case "delete-event":
					admin.DeleteEvent = false
				case "ban-user":
					admin.BanUser = false
				case "add-permission":
					admin.AddPermission = false
				case "remove-permission":
					admin.RemovePermission = false
				}
			}
		}
	}
}
