package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/theplant/htmlgo"
)

func handleHomepage(w http.ResponseWriter, r *http.Request) {
	htmlgo.Fprint(w, homepageHTML(), r.Context())
}

func handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	pubkey := r.PostFormValue("pubkey")
	if pfx, value, err := nip19.Decode(pubkey); err == nil && pfx == "npub" {
		pubkey = value.(string)
	}

	id := make([]byte, 8)
	binary.LittleEndian.PutUint64(id, uint64(time.Now().Unix()))
	groupId := hex.EncodeToString(id[0:4])

	log.Info().Str("id", groupId).Str("owner", pubkey).Msg("making group")

	vrelay := eventstore.RelayWrapper{Store: db}
	res, _ := vrelay.QuerySync(r.Context(), nostr.Filter{Tags: nostr.TagMap{"#h": []string{groupId}}, Limit: 1})
	if len(res) > 0 {
		http.Error(w, "group already exists", 403)
		return
	}

	ownerPermissions := &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      9003,
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey},
			nostr.Tag{"permission", "add-user"},
			nostr.Tag{"permission", "remove-user"},
			nostr.Tag{"permission", "edit-metadata"},
			nostr.Tag{"permission", "add-permission"},
			nostr.Tag{"permission", "remove-permission"},
			nostr.Tag{"permission", "delete-event"},
		},
	}
	if err := ownerPermissions.Sign(s.RelayPrivkey); err != nil {
		http.Error(w, "error signing", 500)
		return
	}

	if err := db.SaveEvent(r.Context(), ownerPermissions); err != nil {
		http.Error(w, "failed to save group creation event", 501)
		return
	}

	applyModerationAction(context.Background(), ownerPermissions)

	naddr, _ := nip19.EncodeEntity(s.RelayPubkey, 39000, groupId, []string{"wss://" + s.Domain})
	fmt.Fprintf(w, "group created!\n\n%s", naddr)
}
