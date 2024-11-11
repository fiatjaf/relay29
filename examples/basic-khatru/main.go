package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/khatru29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

var (
	adminRole     = &nip29.Role{Name: "admin", Description: "the group's max top admin"}
	moderatorRole = &nip29.Role{Name: "moderator", Description: "the person who cleans up unwanted stuff"}
)

func main() {
	relayPrivateKey := nostr.GeneratePrivateKey()

	db := &slicestore.SliceStore{}
	db.Init()

	relay, state := khatru29.Init(relay29.Options{
		Domain:                  "localhost:2929",
		DB:                      db,
		SecretKey:               relayPrivateKey,
		DefaultRoles:            []*nip29.Role{adminRole, moderatorRole},
		GroupCreatorDefaultRole: adminRole,
	})

	// setup group-related restrictions
	state.AllowAction = func(ctx context.Context, group nip29.Group, role *nip29.Role, action relay29.Action) bool {
		// this is simple:
		if _, ok := action.(relay29.PutUser); ok {
			// anyone can invite new users
			return true
		}
		if role == adminRole {
			// owners can do everything
			return true
		}
		if role == moderatorRole {
			// admins can delete people and messages
			switch action.(type) {
			case relay29.RemoveUser:
				return true
			case relay29.DeleteEvent:
				return true
			}
		}
		// no one else can do anything else
		return false
	}

	// init relay
	relay.Info.Name = "very ephemeral chat relay"
	relay.Info.Description = "everything will be deleted as soon as I turn off my computer"

	// extra policies
	relay.RejectEvent = append(relay.RejectEvent,
		policies.PreventLargeTags(64),
		policies.PreventTooManyIndexableTags(6, []int{9005}, nil),
		policies.RestrictToSpecifiedKinds(
			9, 10, 11, 12, 1111,
			30023, 31922, 31923, 9802,
			9000, 9001, 9002, 9003, 9004, 9005, 9006, 9007,
			9021, 9022,
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
