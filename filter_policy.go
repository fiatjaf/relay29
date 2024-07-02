package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

func requireKindAndSingleGroupID(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	if len(filter.IDs) > 0 {
		return false, ""
	}

	if len(filter.Kinds) == 0 {
		return true, "must specify kinds"
	}

	isMeta := false
	isNormal := false
	for _, kind := range filter.Kinds {
		if kind < 10000 {
			isNormal = true
		} else if kind >= 30000 {
			isMeta = true
		}
	}
	if isNormal && isMeta {
		return true, "cannot request both meta and normal events at the same time"
	}
	if !isNormal && !isMeta {
		return true, "unexpected kinds requested"
	}

	if isNormal {
		if tags, _ := filter.Tags["h"]; len(tags) == 0 {
			return true, "must have an 'h' tag"
		}
	}

	return false, ""
}
