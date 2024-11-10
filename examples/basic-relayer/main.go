package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"slices"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/relayer29"
	"github.com/fiatjaf/relayer/v2"
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

	host := "0.0.0.0"
	port := 2929

	opts := relay29.Options{
		Domain:       fmt.Sprintf("%s:%d", host, port),
		DB:           db,
		SecretKey:    relayPrivateKey,
		DefaultRoles: []*nip29.Role{adminRole, moderatorRole},
	}
	relay, state := relayer29.Init(opts)

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

	relay.(*relayer29.Relay).RejectFunc = func(ev *nostr.Event) (bool, string) {
		for _, tag := range ev.Tags {
			if len(tag) > 1 && len(tag[0]) == 1 {
				if len(tag[1]) > 64 {
					return true, "event contains too large tags"
				}
			}
		}
		if ev.Kind == 9005 {
			ntags := 0
			for _, tag := range ev.Tags {
				if len(tag) > 0 && len(tag[0]) == 1 {
					ntags++
					if ntags > 6 {
						return true, "too many indexable tags"
					}
				}
			}
		}
		if !slices.Contains([]int{9, 10, 11, 12, 30023, 31922, 31923, 9802, 9000, 9001, 9002, 9003, 9004, 9005, 9006, 9007, 9021}, ev.Kind) {
			return true, fmt.Sprintf("received event kind %d not allowed", ev.Kind)
		}
		if nostr.Now()-ev.CreatedAt > 60 {
			return true, "event too old"
		}
		if ev.CreatedAt-nostr.Now() > 30 {
			return true, "event too much in the future"
		}
		return false, ""
	}

	server, err := relayer.NewServer(
		relay,
		relayer.WithPerConnectionLimiter(5.0, 1),
	)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	// http routes
	server.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "nothing to see here, you must use a nip-29 powered client")
	})

	fmt.Printf("running on http://%v\n", opts.Domain)
	if err := server.Start(host, port); err != nil {
		log.Fatal("failed to serve")
	}
}
