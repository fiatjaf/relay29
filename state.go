package relay29

import (
	"context"
	"fmt"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
)

type InviteCode struct {
	Code       string
	GroupID    string
	Creator    string
	CreatedAt  nostr.Timestamp
	Expiration *nostr.Timestamp
}

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

	// Store invite codes: map[code]InviteCode
	InviteCodes *xsync.MapOf[string, InviteCode]

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

	// we keep invite codes in memory
	inviteCodes := xsync.NewMapOf[string, InviteCode]()

	state := &State{
		Domain: opts.Domain,
		Groups: groups,
		DB:     opts.DB,

		AllowPrivateGroups: true,

		InviteCodes: inviteCodes,

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

	// start periodic cleanup of expired invite codes
	go state.cleanupExpiredInviteCodes()

	return state
}

// cleanupExpiredInviteCodes runs periodically to remove expired invite codes
func (s *State) cleanupExpiredInviteCodes() {
	ticker := time.NewTicker(5 * time.Minute) // cleanup every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		now := nostr.Now()
		s.InviteCodes.Range(func(code string, invite InviteCode) bool {
			if invite.Expiration != nil && now > *invite.Expiration {
				s.InviteCodes.Delete(code)
			}
			return true
		})
	}
}

// GetValidInviteCode returns the invite code if it exists and is not expired
func (s *State) GetValidInviteCode(code string) (InviteCode, bool) {
	invite, exists := s.InviteCodes.Load(code)
	if !exists {
		return InviteCode{}, false
	}

	// Check if expired
	if invite.Expiration != nil && nostr.Now() > *invite.Expiration {
		s.InviteCodes.Delete(code)
		return InviteCode{}, false
	}

	return invite, true
}
