package relay29

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

type Group struct {
	nip29.Group
	mu sync.RWMutex

	last50      []string
	last50index atomic.Int32
}

// NewGroup creates a new group from scratch (but doesn't store it in the groups map)
func (s *State) NewGroup(id string, creator string) *Group {
	group := &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: "wss://" + s.Domain,
			},
			Roles:   s.defaultRoles,
			Members: make(map[string][]*nip29.Role, 12),
		},
		last50: make([]string, 50),
	}

	group.Members[creator] = []*nip29.Role{s.groupCreatorDefaultRole}

	return group
}

// loadGroupsFromDB loads all the group metadata from all the past action messages.
func (s *State) loadGroupsFromDB(ctx context.Context) error {
	groupMetadataEvents, err := s.DB.QueryEvents(ctx, nostr.Filter{Kinds: []int{nostr.KindSimpleGroupCreateGroup}})
	if err != nil {
		return err
	}
	for evt := range groupMetadataEvents {
		gtag := evt.Tags.GetFirst([]string{"h", ""})
		id := (*gtag)[1]

		group := s.NewGroup(id, evt.PubKey)
		f := nostr.Filter{
			Limit: 5000, Kinds: nip29.ModerationEventKinds, Tags: nostr.TagMap{"h": []string{id}},
		}
		ch, err := s.DB.QueryEvents(ctx, f)
		if err != nil {
			return err
		}

		events := make([]*nostr.Event, 0, 5000)
		for event := range ch {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, err := PrepareModerationAction(evt)
			if err != nil {
				return err
			}
			act.Apply(&group.Group, s)
		}

		// if the group was deleted there will be no actions after the delete
		if len(events) > 0 && events[0].Kind == nostr.KindSimpleGroupDeleteGroup {
			// we don't keep track of this if it was deleted
			continue
		}

		// load the last 50 event ids for "previous" tag checking
		i := 49
		ch, err = s.DB.QueryEvents(ctx, nostr.Filter{Tags: nostr.TagMap{"h": []string{id}}, Limit: 50})
		if err != nil {
			return err
		}
		for evt := range ch {
			group.last50[i] = evt.ID
			i--
		}

		s.Groups.Store(group.Address.ID, group)
	}

	return nil
}

func (s *State) GetGroupFromEvent(event *nostr.Event) *Group {
	group, _ := s.Groups.Load(GetGroupIDFromEvent(event))
	return group
}

func GetGroupIDFromEvent(event *nostr.Event) string {
	gtag := event.Tags.GetFirst([]string{"h", ""})
	groupId := (*gtag)[1]
	return groupId
}
