package main

import (
	"context"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

func accept(event *nostr.Event) (reject bool, msg string) {
	if nip29.MetadataEventKinds.Includes(event.Kind) {
		return true, "can't write metadata event kinds directly"
	}

	for _, re := range []func(ctx context.Context, event *nostr.Event) (reject bool, msg string){
		state.RequireHTagForExistingGroup,
		state.RequireModerationEventsToBeRecent,
		state.RestrictWritesBasedOnGroupRules,
		state.RestrictInvalidModerationActions,
		state.PreventWritingOfEventsJustDeleted,
		state.CheckPreviousTag,
	} {
		if reject, msg := re(ctx, event); reject {
			return reject, msg
		}
	}

	return false, ""
}
