package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func main() {
	relayPrivateKey := nostr.GeneratePrivateKey()

	state := relay29.Init(relay29.Options{
		Domain:    "localhost:2929",
		DB:        &slicestore.SliceStore{},
		SecretKey: relayPrivateKey,
	})

	// init relay
	state.Relay.Info.Name = "very ephemeral chat relay"
	state.Relay.Info.PubKey, _ = nostr.GetPublicKey(relayPrivateKey)
	state.Relay.Info.Description = "everything will be deleted as soon as I turn off my computer"

	// extra policies
	state.Relay.RejectEvent = slices.Insert(state.Relay.RejectEvent, 0,
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
	state.Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "nothing to see here, you must use a nip-29 powered client")
	})

	fmt.Println("running on http://0.0.0.0:2929")
	if err := http.ListenAndServe(":2929", state.Relay); err != nil {
		log.Fatal("failed to serve")
	}
}
