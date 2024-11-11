package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/fiatjaf/relay29"
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

	if err := state.CreateGroup(r.Context(), groupId, pubkey, relay29.EditMetadata{NameValue: &name}); err != nil {
		http.Error(w, "failed to create group: "+err.Error(), 400)
		return
	}

	group, _ := state.Groups.Load(groupId)
	naddr, _ := nip19.EncodeEntity(s.RelayPubkey, 39000, groupId, []string{"wss://" + s.Domain})
	fmt.Fprintf(w, "group created!\n\n%s\naddress: %s", naddr, group.Address)
}
