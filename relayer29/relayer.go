package relayer29

import (
	"context"
	"errors"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relayer/v2"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
)

type Relay struct {
	NIP11Info  func() nip11.RelayInformationDocument
	RejectFunc func(*nostr.Event) (bool, string)
	pubkey     string
	opts       relay29.Options
	state      *relay29.State
}

func (r *Relay) Name() string {
	return "nostr-relay29"
}

func (r *Relay) Init() error {
	return nil
}

func (r *Relay) Storage(ctx context.Context) eventstore.Store {
	return &Store{
		relay: r,
		state: r.state,
		store: r.opts.DB,
	}
}

func (r *Relay) AcceptEvent(ctx context.Context, ev *nostr.Event) bool {
	return true
}

func (r *Relay) GetNIP11InformationDocument() nip11.RelayInformationDocument {
	if r.NIP11Info != nil {
		return r.NIP11Info()
	}

	return nip11.RelayInformationDocument{
		Name:          "nostr-relay29",
		Description:   "relay29 rleay powered by the relayer framework",
		SupportedNIPs: []int{29},
	}
}

func (r *Relay) BroadcastEvent(ev *nostr.Event) {
	relayer.BroadcastEvent(ev)
}

func (r *Relay) AddEvent(ctx context.Context, ev *nostr.Event) (skipBroadcast bool, writeError error) {
	err := r.opts.DB.SaveEvent(ctx, ev)
	return true, err
}

func Init(opts relay29.Options) (relayer.Relay, *relay29.State) {
	pubkey, _ := nostr.GetPublicKey(opts.SecretKey)

	// create a new relay29.State
	state := relay29.New(opts)

	// create a new khatru relay
	relay := &Relay{
		pubkey: pubkey,
		state:  state,
		opts:   opts,
	}

	// assign khatru relay to relay29.State
	state.Relay = relay

	// provide GetAuthed function
	state.GetAuthed = func(context.Context) string {
		// TODO
		return ""
	}

	return relay, state
}

type Store struct {
	relay *Relay
	state *relay29.State
	store eventstore.Store
}

func (s *Store) Init() error {
	return s.store.Init()
}

func (s *Store) Close() {
	s.store.Close()
}

func (s *Store) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	if rejected, msg := s.state.RequireKindAndSingleGroupIDOrSpecificEventReference(ctx, filter); rejected {
		return nil, errors.New(msg)
	}

	rfs := []func(context.Context, nostr.Filter) (chan *nostr.Event, error){
		s.state.NormalEventQuery,
		s.state.MetadataQueryHandler,
		s.state.AdminsQueryHandler,
		s.state.MembersQueryHandler,
		s.state.RolesQueryHandler,
	}
	for _, rf := range rfs {
		if evc, err := rf(ctx, filter); err == nil {
			return evc, nil
		}
	}
	return s.store.QueryEvents(ctx, filter)
}

func (s *Store) DeleteEvent(ctx context.Context, ev *nostr.Event) error {
	return s.store.DeleteEvent(ctx, ev)
}

func (s *Store) SaveEvent(ctx context.Context, ev *nostr.Event) error {
	if s.relay.RejectFunc != nil {
		if rejected, msg := s.relay.RejectFunc(ev); rejected {
			return errors.New(msg)
		}
	}

	bfs := []func(context.Context, *nostr.Event) (bool, string){
		s.state.RequireHTagForExistingGroup,
		s.state.RequireModerationEventsToBeRecent,
		s.state.RestrictWritesBasedOnGroupRules,
		s.state.RestrictInvalidModerationActions,
		s.state.PreventWritingOfEventsJustDeleted,
		s.state.CheckPreviousTag,
	}
	for _, rf := range bfs {
		if rejected, msg := rf(ctx, ev); rejected {
			return errors.New(msg)
		}
	}

	err := s.store.SaveEvent(ctx, ev)
	if err != nil {
		return err
	}

	afs := []func(context.Context, *nostr.Event){
		s.state.ApplyModerationAction,
		s.state.ReactToJoinRequest,
		s.state.ReactToLeaveRequest,
		s.state.AddToPreviousChecking,
	}
	for _, rf := range afs {
		rf(ctx, ev)
	}

	return err
}
