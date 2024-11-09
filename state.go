package relay29

import (
	"context"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
)

type State struct {
	Domain string
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store
	Relay  interface {
		BroadcastEvent(*nostr.Event)
		AddEvent(context.Context, *nostr.Event) (skipBroadcast bool, writeError error)
	}
	GetAuthed func(context.Context) string

	AllowPrivateGroups bool

	deletedCache set.Set[string]
	publicKey    string
	secretKey    string

	AllowAction struct {
		CreateGroup  func(context.Context, nip29.Group, *nip29.Role, CreateGroup) bool
		PutUser      func(context.Context, nip29.Group, *nip29.Role, PutUser) bool
		RemoveUser   func(context.Context, nip29.Group, *nip29.Role, RemoveUser) bool
		EditMetadata func(context.Context, nip29.Group, *nip29.Role, EditMetadata) bool
		DeleteEvent  func(context.Context, nip29.Group, *nip29.Role, DeleteEvent) bool
		DeleteGroup  func(context.Context, nip29.Group, *nip29.Role, DeleteGroup) bool
	}
}

type Options struct {
	Domain    string
	DB        eventstore.Store
	SecretKey string
}

func New(opts Options) *State {
	pubkey, _ := nostr.GetPublicKey(opts.SecretKey)

	// events that just got deleted will be cached here for `tooOld` seconds such that someone doesn't rebroadcast
	// them -- after that time we won't accept them anymore, so we can remove their ids from this cache
	deletedCache := set.NewSliceSet[string]()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &State{
		Domain: opts.Domain,
		Groups: groups,
		DB:     opts.DB,

		AllowPrivateGroups: true,

		deletedCache: deletedCache,
		publicKey:    pubkey,
		secretKey:    opts.SecretKey,
	}

	// load all groups
	state.loadGroups(context.Background())

	return state
}
