package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/nbd-wtf/go-nostr/nip29"
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

	group := getGroup(groupId)
	if group != nil {
		http.Error(w, "group already exists", 403)
		return
	}

	// create group right here
	group = newGroup(groupId)
	addGroup(group)

	ownerPermissions := &nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      9003,
		Tags: nostr.Tags{
			nostr.Tag{"h", groupId},
			nostr.Tag{"p", pubkey},
			nostr.Tag{"permission", nip29.PermAddUser},
			nostr.Tag{"permission", nip29.PermRemoveUser},
			nostr.Tag{"permission", nip29.PermEditMetadata},
			nostr.Tag{"permission", nip29.PermAddPermission},
			nostr.Tag{"permission", nip29.PermRemovePermission},
			nostr.Tag{"permission", nip29.PermDeleteEvent},
			nostr.Tag{"permission", nip29.PermEditGroupStatus},
		},
	}
	if err := ownerPermissions.Sign(s.RelayPrivkey); err != nil {
		log.Error().Err(err).Msg("error signing group creation event")
		http.Error(w, "error signing group creation event: "+err.Error(), 500)
		return
	}

	if err := relay.AddEvent(r.Context(), ownerPermissions); err != nil {
		log.Error().Err(err).Stringer("event", ownerPermissions).Msg("failed to save group creation event")
		http.Error(w, "failed to save group creation event", 501)
		return
	}

	naddr, _ := nip19.EncodeEntity(s.RelayPubkey, 39000, groupId, []string{"wss://" + s.Domain})
	fmt.Fprintf(w, "group created!\n\n%s\nid: %s", naddr, groupId)
}
