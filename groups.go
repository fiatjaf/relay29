package relay29

import (
	"context"
	"sync"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

type Group struct {
	nip29.Group
	mu sync.RWMutex
}

func (s *State) NewGroup(id string) *Group {
	return &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: "wss://" + s.Domain,
			},
			Members: map[string]*nip29.Role{},
		},
	}
}

// loadGroups loads all the group metadata from all the past action messages.
func (s *State) loadGroups(ctx context.Context) {
	groupMetadataEvents, _ := s.DB.QueryEvents(ctx, nostr.Filter{Kinds: []int{nostr.KindSimpleGroupCreateGroup}})
	for evt := range groupMetadataEvents {
		gtag := evt.Tags.GetFirst([]string{"h", ""})
		id := (*gtag)[1]

		group := s.NewGroup(id)
		f := nostr.Filter{
			Limit: 5000, Kinds: nip29.ModerationEventKinds, Tags: nostr.TagMap{"h": []string{id}},
		}
		ch, _ := s.DB.QueryEvents(ctx, f)

		events := make([]*nostr.Event, 0, 5000)
		for event := range ch {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, _ := PrepareModerationAction(evt)
			act.Apply(&group.Group)
		}

		// if the group was deleted there will be no actions after the delete
		if len(events) > 0 && events[0].Kind == nostr.KindSimpleGroupDeleteGroup {
			// we don't keep track of this if it was deleted
			continue
		}

		s.Groups.Store(group.Address.ID, group)
	}
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
