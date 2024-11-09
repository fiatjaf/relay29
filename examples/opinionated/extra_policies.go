package main

import (
	"context"
	"time"

	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/puzpuzpuz/xsync/v3"
	"golang.org/x/time/rate"
)

func preventGroupCreation(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if event.Kind == 9007 && !relay29.IsInternalCall(ctx) {
		return true, "to create groups open https://" + s.Domain + " in your web browser"
	}
	return false, ""
}

func blockDeletesOfOldMessages(ctx context.Context, target, deletion *nostr.Event) (acceptDeletion bool, msg string) {
	if target.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
		return false, "can't delete old event, contact relay admin"
	}

	return true, ""
}

// very strict rate limits
var rateLimitBuckets = xsync.NewMapOf[*relay29.Group, *rate.Limiter]()

func rateLimit(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	group := state.GetGroupFromEvent(event)

	bucket, _ := rateLimitBuckets.LoadOrCompute(group, func() *rate.Limiter {
		return rate.NewLimiter(rate.Every(time.Minute*2), 15)
	})

	if rsv := bucket.Reserve(); rsv.Delay() != 0 {
		rsv.Cancel()
		return true, "rate-limited"
	} else {
		rsv.OK()
		return
	}
}
