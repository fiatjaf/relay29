package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func main() {
	relayPrivateKey := nostr.GeneratePrivateKey()

	db := &slicestore.SliceStore{}
	db.Init()

	pubkey, _ := nostr.GetPublicKey(relayPrivateKey)

	// we create a new khatru relay
	relay := khatru.NewRelay()
	relay.Info.PubKey = pubkey
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)

	state := relay29.Init(relay29.Options{
		Relay:     relay,
		Domain:    "localhost:2929",
		DB:        db,
		SecretKey: relayPrivateKey,
	})

	// apply basic relay policies
	relay.StoreEvent = append(relay.StoreEvent, state.DB.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents,
		state.NormalEventQuery,
		state.MetadataQueryHandler,
		state.AdminsQueryHandler,
		state.MembersQueryHandler,
	)
	relay.DeleteEvent = append(relay.DeleteEvent, state.DB.DeleteEvent)
	relay.RejectFilter = append(relay.RejectFilter,
		state.RequireKindAndSingleGroupIDOrSpecificEventReference,
	)
	relay.RejectEvent = append(relay.RejectEvent,
		state.RequireModerationEventsToBeRecent,
		state.RequireHTagForExistingGroup,
		state.RestrictWritesBasedOnGroupRules,
		state.RestrictInvalidModerationActions,
		state.PreventWritingOfEventsJustDeleted,
	)
	relay.OnEventSaved = append(relay.OnEventSaved,
		state.ApplyModerationAction,
		state.ReactToJoinRequest,
	)
	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)

	// init relay
	relay.Info.Name = "very ephemeral chat relay"
	relay.Info.Description = "everything will be deleted as soon as I turn off my computer"

	// extra policies
	relay.RejectEvent = slices.Insert(relay.RejectEvent, 0,
		policies.PreventLargeTags(64),
		policies.PreventTooManyIndexableTags(6, []int{9005}, nil),
		policies.RestrictToSpecifiedKinds(
			9, 10, 11, 12,
			30023, 31922, 31923, 9802,
			9000, 9001, 9002, 9003, 9004, 9005, 9006, 9007,
			9021,
		),
		policies.PreventTimestampsInThePast(60),
		policies.PreventTimestampsInTheFuture(30),
	)

	// http routes
	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "nothing to see here, you must use a nip-29 powered client")
	})

	fmt.Println("running on http://0.0.0.0:2929")
	if err := http.ListenAndServe(":2929", relay); err != nil {
		log.Fatal("failed to serve")
	}
}
