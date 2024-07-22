package main

import (
	"net/http"
	"os"
	"slices"

	"github.com/fiatjaf/eventstore/bolt"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/policies"
	"github.com/fiatjaf/relay29"
	"github.com/fiatjaf/relay29/khatru29"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
	"github.com/rs/zerolog"
)

type Settings struct {
	Port             string `envconfig:"PORT" default:"5577"`
	Domain           string `envconfig:"DOMAIN" required:"true"`
	RelayName        string `envconfig:"RELAY_NAME" required:"true"`
	RelayPrivkey     string `envconfig:"RELAY_PRIVKEY" required:"true"`
	RelayDescription string `envconfig:"RELAY_DESCRIPTION"`
	RelayContact     string `envconfig:"RELAY_CONTACT"`
	RelayIcon        string `envconfig:"RELAY_ICON"`
	DatabasePath     string `envconfig:"DATABASE_PATH" default:"./db"`

	RelayPubkey string `envconfig:"-"`
}

var (
	s     Settings
	db    = &bolt.BoltBackend{}
	log   = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	relay *khatru.Relay
	state *relay29.State
)

func main() {
	err := envconfig.Process("", &s)
	if err != nil {
		log.Fatal().Err(err).Msg("couldn't process envconfig")
		return
	}
	s.RelayPubkey, _ = nostr.GetPublicKey(s.RelayPrivkey)

	// load db
	db.Path = s.DatabasePath
	if err := db.Init(); err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
		return
	}
	log.Debug().Str("path", db.Path).Msg("initialized database")

	// init relay29 stuff
	relay, state = khatru29.Init(relay29.Options{
		Domain:    s.Domain,
		DB:        db,
		SecretKey: s.RelayPrivkey,
	})

	// init relay
	relay.Info.Name = s.RelayName
	relay.Info.Description = s.RelayDescription
	relay.Info.Contact = s.RelayContact
	relay.Info.Icon = s.RelayIcon

	relay.OverwriteDeletionOutcome = append(relay.OverwriteDeletionOutcome,
		blockDeletesOfOldMessages,
	)
	relay.RejectEvent = slices.Insert(relay.RejectEvent, 2,
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
		rateLimit,
		preventGroupCreation,
	)

	// http routes
	relay.Router().HandleFunc("/create", handleCreateGroup)
	relay.Router().HandleFunc("/", handleHomepage)

	log.Info().Str("relay-pubkey", s.RelayPubkey).Msg("running on http://0.0.0.0:" + s.Port)
	if err := http.ListenAndServe(":"+s.Port, relay); err != nil {
		log.Fatal().Err(err).Msg("failed to serve")
	}
}
