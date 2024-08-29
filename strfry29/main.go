package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fiatjaf/eventstore/strfry"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
)

var (
	state *relay29.State
	ctx   = context.Background()

	strfrydb strfry.StrfryBackend
)

func main() {
	incoming := json.NewDecoder(os.Stdin)
	outgoing := json.NewEncoder(os.Stdout)

	curr, _ := os.Getwd()
	path := filepath.Join(curr, "strfry29.json")
	confb, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("couldn't open config file at %s: %s", path, err)
		return
	}

	var conf struct {
		Domain           string `json:"domain"`
		RelaySecretKey   string `json:"relay_secret_key"`
		StrfryConfig     string `json:"strfry_config_path"`
		StrfryExecutable string `json:"strfry_executable_path"`
	}
	if err := json.Unmarshal(confb, &conf); err != nil {
		log.Fatalf("invalid json config at %s: %s", path, err)
		return
	}

	strfrydb = strfry.StrfryBackend{
		ConfigPath:     conf.StrfryConfig,
		ExecutablePath: conf.StrfryExecutable,
	}
	strfrydb.Init()
	defer strfrydb.Close()

	state = relay29.New(relay29.Options{
		Domain:    conf.Domain,
		DB:        &strfrydb,
		SecretKey: conf.RelaySecretKey,
	})

	state.AllowPrivateGroups = false
	state.GetAuthed = func(ctx context.Context) string { return "" }
	state.Relay = protoRelay{}

	// rebuild metadata events (replaceable) for all groups and make them available
	filter := nostr.Filter{Kinds: []int{nostr.KindSimpleGroupMetadata, nostr.KindSimpleGroupAdmins, nostr.KindSimpleGroupMembers}}
	if err := republishMetadataEvents(filter); err != nil {
		log.Fatalf("failed to republish metadata events on startup: %s", err)
		return
	}

	for {
		var msg StrfryMessage

		err := incoming.Decode(&msg)
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Print("[strfry29] failed to decode request. killing: " + err.Error())
			return
		}

		// message, _ := json.Marshal(msg)
		// log.Print("[strfry29] got event: ", string(message))

		if reject, rejectMsg := accept(msg.Event); reject {
			outgoing.Encode(StrfryResponse{
				ID:     msg.Event.ID,
				Action: "reject",
				Msg:    rejectMsg,
			})
		} else {
			outgoing.Encode(StrfryResponse{
				ID:     msg.Event.ID,
				Action: "accept",
			})

			go func() {
				time.Sleep(time.Millisecond * 200)

				state.ApplyModerationAction(ctx, msg.Event)
				state.ReactToJoinRequest(ctx, msg.Event)
				state.ReactToLeaveRequest(ctx, msg.Event)
			}()
		}
	}
}

type StrfryMessage struct {
	Type       string       `json:"type"`
	Event      *nostr.Event `json:"event"`
	SourceType string       `json:"sourceType"`
}

type StrfryResponse struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Msg    string `json:"msg"`
}

type protoRelay struct{}

func (p protoRelay) AddEvent(ctx context.Context, evt *nostr.Event) (skipBroadcast bool, writeError error) {
	err := strfrydb.SaveEvent(ctx, evt)
	return false, err
}

func (p protoRelay) BroadcastEvent(evt *nostr.Event) {
	strfrydb.SaveEvent(ctx, evt)
}
