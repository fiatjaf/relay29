package main

import (
	"net/http"
	"os"

	"github.com/fiatjaf/eventstore/lmdb"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/plugins"
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
	db    = lmdb.LMDBBackend{}
	log   = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	relay = khatru.NewRelay()
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

	// init relay
	relay.Name = s.RelayName
	relay.PubKey = s.RelayPubkey
	relay.Description = s.RelayDescription
	relay.Contact = s.RelayContact
	relay.IconURL = s.RelayIcon

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents,
		db.QueryEvents,
		metadataQueryHandler,
		adminsQueryHandler,
	)
	relay.CountEvents = append(relay.CountEvents, db.CountEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.OverwriteDeletionOutcome = append(relay.OverwriteDeletionOutcome,
		blockDeletesOfOldMessages,
	)
	relay.OverwriteFilter = append(relay.OverwriteFilter,
		plugins.RemoveAllButKinds(9, 11, 9001, 9002, 9003, 9004, 9005, 39000, 39001),
	)
	relay.RejectFilter = append(relay.RejectFilter,
		requireKindAndSingleGroupID,
	)
	relay.RejectEvent = append(relay.RejectEvent,
		plugins.RestrictToSpecifiedKinds(9, 11, 9000, 9001, 9002, 9003, 9004, 9005),
		plugins.PreventTimestampsInThePast(60),
		plugins.PreventTimestampsInTheFuture(30),
		requireHTag,
		restrictWritesBasedOnGroupRules,
		restrictInvalidModerationActions,
		rateLimit,
	)
	relay.OnEventSaved = append(relay.OnEventSaved,
		applyModerationAction,
	)

	log.Info().Msg("running on http://0.0.0.0:" + s.Port)
	if err := http.ListenAndServe(":"+s.Port, relay); err != nil {
		log.Fatal().Err(err).Msg("failed to serve")
	}
}
