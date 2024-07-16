package relay29

import (
	"context"
	"sync"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
	"github.com/puzpuzpuz/xsync/v3"
)

type Relay interface {
	BroadcastEvent(*nostr.Event)
	AddEvent(context.Context, *nostr.Event) (bool, error)
}

type State struct {
	Relay  Relay
	Domain string
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store

	deletedCache set.Set[string]
	publicKey    string
	privateKey   string
}

type Options struct {
	Relay     Relay
	Domain    string
	DB        eventstore.Store
	SecretKey string
}

func Init(opts Options) *State {
	pubkey, _ := nostr.GetPublicKey(opts.SecretKey)

	// events that just got deleted will be cached here for `tooOld` seconds such that someone doesn't rebroadcast
	// them -- after that time we won't accept them anymore, so we can remove their ids from this cache
	deletedCache := set.NewSliceSet[string]()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &State{
		opts.Relay,
		opts.Domain,
		groups,
		opts.DB,
		deletedCache,
		pubkey,
		opts.SecretKey,
	}

	// load all groups
	state.loadGroups(context.Background())

	return state
}

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
			act, _ := nip29_relay.GetModerationAction(evt)
			act.Apply(&group.Group)
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
