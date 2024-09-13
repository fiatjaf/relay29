package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/khatru29"
	"github.com/nbd-wtf/go-nostr"
)

func main() {
	relayPrivateKey := nostr.GeneratePrivateKey()

	db := &slicestore.SliceStore{}
	db.Init()

	relay, _ := khatru29.Init(relay29.Options{
		Domain:    "localhost:2929",
		DB:        db,
		SecretKey: relayPrivateKey,
	})

	// init relay
	relay.Info.Name = "very ephemeral chat relay"
	relay.Info.Description = "everything will be deleted as soon as I turn off my computer"

	// extra policies
	relay.RejectEvent = append(relay.RejectEvent,
		policies.PreventLargeTags(64),
		policies.PreventTooManyIndexableTags(6, []int{9005}, nil),
		policies.RestrictToSpecifiedKinds(
			9, 10, 11, 12,
			30023, 31922, 31923, 9802,
			9000, 9001, 9002, 9003, 9004, 9005, 9006, 9007,
			9021,
		),
		policies.PreventTimestampsInThePast(60*time.Second),
		policies.PreventTimestampsInTheFuture(30*time.Second),
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
