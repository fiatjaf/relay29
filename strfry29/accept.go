package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
)

func accept(event *nostr.Event) (reject bool, msg string) {
	ctx := context.Background()

	for _, re := range []func(ctx context.Context, event *nostr.Event) (reject bool, msg string){
		state.RequireHTagForExistingGroup,
		state.RequireModerationEventsToBeRecent,
		state.RestrictWritesBasedOnGroupRules,
		state.RestrictInvalidModerationActions,
		state.PreventWritingOfEventsJustDeleted,
	} {
		if reject, msg := re(ctx, event); reject {
			return reject, msg
		}
	}

	return false, ""
}
