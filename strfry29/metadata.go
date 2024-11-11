package main

import (
	"context"
	"fmt"

	"github.com/nbd-wtf/go-nostr"
)

func republishMetadataEvents(basefilter nostr.Filter) error {
	filter := basefilter

	filter.Kinds = []int{nostr.KindSimpleGroupMetadata}
	if err := republishMetadataEvent(state.MetadataQueryHandler, filter); err != nil {
		return fmt.Errorf("with filter %s: %w", filter, err)
	}

	filter.Kinds = []int{nostr.KindSimpleGroupAdmins}
	if err := republishMetadataEvent(state.AdminsQueryHandler, filter); err != nil {
		return fmt.Errorf("with filter %s: %w", filter, err)
	}

	filter.Kinds = []int{nostr.KindSimpleGroupMembers}
	if err := republishMetadataEvent(state.MembersQueryHandler, filter); err != nil {
		return fmt.Errorf("with filter %s: %w", filter, err)
	}

	filter.Kinds = []int{nostr.KindSimpleGroupRoles}
	if err := republishMetadataEvent(state.MembersQueryHandler, filter); err != nil {
		return fmt.Errorf("with filter %s: %w", filter, err)
	}

	return nil
}

func republishMetadataEvent(querier func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error), filter nostr.Filter) error {
	ch, err := querier(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to build: %s", err)
	}

	for evt := range ch {
		if err := strfrydb.SaveEvent(ctx, evt); err != nil {
			return fmt.Errorf("failed to publish: %w", err)
		}
	}

	return nil
}
