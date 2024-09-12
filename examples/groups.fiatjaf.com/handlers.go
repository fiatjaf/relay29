package main

import (
	"context"
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
	if !nostr.IsValidPublicKey(pubkey) {
		http.Error(w, "pubkey is invalid", 400)
		return
	}

	name := r.PostFormValue("name")
	if name == "" {
		http.Error(w, "name is empty", 400)
		return
	}

	question := r.PostFormValue("captcha-id")
	solution := r.PostFormValue("captcha-solution")
	if !captcha.Verify(question, solution, true) {
		log.Info().Str("solution", solution).Msg("invalid captcha")
		http.Error(w, "captcha solution is wrong", 400)
		return
	}

	id := make([]byte, 8)
	binary.LittleEndian.PutUint64(id, uint64(time.Now().Unix()))
	groupId := hex.EncodeToString(id[0:3])

	log.Info().Str("id", groupId).Str("owner", pubkey).Msg("making group")

	group, _ := state.Groups.Load(groupId)
	if group != nil {
		http.Error(w, "group already exists", 403)
		return
	}

	foundingEvents := []*nostr.Event{
		{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupCreateGroup,
			Tags: nostr.Tags{
				nostr.Tag{"h", groupId},
			},
		},
		{
			CreatedAt: nostr.Now() + 1,
			Kind:      nostr.KindSimpleGroupAddPermission,
			Tags: nostr.Tags{
				nostr.Tag{"h", groupId},
				nostr.Tag{"p", pubkey},
				nostr.Tag{"permission", string(nip29.PermAddUser)},
				nostr.Tag{"permission", string(nip29.PermRemoveUser)},
				nostr.Tag{"permission", string(nip29.PermEditMetadata)},
				nostr.Tag{"permission", string(nip29.PermAddPermission)},
				nostr.Tag{"permission", string(nip29.PermRemovePermission)},
				nostr.Tag{"permission", string(nip29.PermDeleteEvent)},
				nostr.Tag{"permission", string(nip29.PermEditGroupStatus)},
			},
		},
		{
			CreatedAt: nostr.Now() + 2,
			Kind:      nostr.KindSimpleGroupEditMetadata,
			Tags: nostr.Tags{
				nostr.Tag{"h", groupId},
				nostr.Tag{"name", name},
			},
		},
	}

	ourCtx := context.WithValue(r.Context(), internalCallContextKey, &struct{}{})

	for _, evt := range foundingEvents {
		if err := evt.Sign(s.RelayPrivkey); err != nil {
			log.Error().Err(err).Msg("error signing group creation event")
			http.Error(w, "error signing group creation event: "+err.Error(), 500)
			return
		}
		if _, err := state.Relay.AddEvent(ourCtx, evt); err != nil {
			log.Error().Err(err).Stringer("event", evt).Msg("failed to save group creation event")
			http.Error(w, "failed to save group creation event", 501)
			return
		}
	}

	group, _ = state.Groups.Load(groupId)
	naddr, _ := nip19.EncodeEntity(s.RelayPubkey, 39000, groupId, []string{"wss://" + s.Domain})
	fmt.Fprintf(w, "group created!\n\n%s\naddress: %s", naddr, group.Address)
}
