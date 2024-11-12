package relay29

import (
	"context"
	"fmt"

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

	deletedCache            set.Set[string]
	publicKey               string
	secretKey               string
	defaultRoles            []*nip29.Role
	groupCreatorDefaultRole *nip29.Role

	AllowAction func(ctx context.Context, group nip29.Group, role *nip29.Role, action Action) bool
}

type Options struct {
	Domain                  string
	DB                      eventstore.Store
	SecretKey               string
	DefaultRoles            []*nip29.Role
	GroupCreatorDefaultRole *nip29.Role
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

		deletedCache:            deletedCache,
		publicKey:               pubkey,
		secretKey:               opts.SecretKey,
		defaultRoles:            opts.DefaultRoles,
		groupCreatorDefaultRole: opts.GroupCreatorDefaultRole,
	}

	// load all groups
	err := state.loadGroupsFromDB(context.Background())
	if err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}
