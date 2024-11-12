package khatru29

import (
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

func Init(opts relay29.Options) (*khatru.Relay, *relay29.State) {
	pubkey, _ := nostr.GetPublicKey(opts.SecretKey)

	// create a new relay29.State
	state := relay29.New(opts)

	// create a new khatru relay
	relay := khatru.NewRelay()
	relay.Info.PubKey = pubkey
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)

	// assign khatru relay to relay29.State
	state.Relay = relay

	// provide GetAuthed function
	state.GetAuthed = khatru.GetAuthed

	// apply basic relay policies
	relay.StoreEvent = append(relay.StoreEvent, state.DB.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents,
		state.NormalEventQuery,
		state.MetadataQueryHandler,
		state.AdminsQueryHandler,
		state.MembersQueryHandler,
		state.RolesQueryHandler,
	)
	relay.DeleteEvent = append(relay.DeleteEvent, state.DB.DeleteEvent)
	relay.RejectFilter = append(relay.RejectFilter,
		state.RequireKindAndSingleGroupIDOrSpecificEventReference,
	)
	relay.RejectEvent = append(relay.RejectEvent,
		state.RequireHTagForExistingGroup,
		state.RequireModerationEventsToBeRecent,
		state.RestrictWritesBasedOnGroupRules,
		state.RestrictInvalidModerationActions,
		state.PreventWritingOfEventsJustDeleted,
		state.CheckPreviousTag,
	)
	relay.OnEventSaved = append(relay.OnEventSaved,
		state.ApplyModerationAction,
		state.ReactToJoinRequest,
		state.ReactToLeaveRequest,
		state.AddToPreviousChecking,
	)
	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)

	return relay, state
}
