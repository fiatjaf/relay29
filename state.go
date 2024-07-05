package relay29

import (
	"context"
	"sync"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	nip29_relay "github.com/nbd-wtf/go-nostr/nip29/relay"
	"github.com/puzpuzpuz/xsync/v3"
)

type State struct {
	Relay  *khatru.Relay
	Domain string
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store

	deletedCache set.Set[string]
	publicKey    string
	privateKey   string
}

type Options struct {
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

	// we create a new khatru relay
	relay := khatru.NewRelay()
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)

	state := &State{
		relay,
		opts.Domain,
		groups,
		opts.DB,
		deletedCache,
		pubkey,
		opts.SecretKey,
	}

	// load all groups
	state.loadGroups(context.Background())

	// apply basic relay policies
	relay.StoreEvent = append(relay.StoreEvent, state.DB.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents,
		state.normalEventQuery,
		state.metadataQueryHandler,
		state.adminsQueryHandler,
		state.membersQueryHandler,
	)
	relay.DeleteEvent = append(relay.DeleteEvent, state.DB.DeleteEvent)
	relay.RejectFilter = append(relay.RejectFilter,
		state.requireKindAndSingleGroupIDOrSpecificEventReference,
	)
	relay.RejectEvent = append(relay.RejectEvent,
		state.requireHTagForExistingGroup,
		state.restrictWritesBasedOnGroupRules,
		state.restrictInvalidModerationActions,
		state.preventWritingOfEventsJustDeleted,
	)
	relay.OnEventSaved = append(relay.OnEventSaved,
		state.applyModerationAction,
		state.reactToJoinRequest,
	)
	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)

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
