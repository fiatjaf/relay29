package relay29

import (
	"context"
	"slices"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

func (s *State) RequireKindAndSingleGroupIDOrSpecificEventReference(
	ctx context.Context,
	filter nostr.Filter,
) (reject bool, msg string) {
	isMeta := false
	isNormal := false
	isReference := false

	for _, kind := range filter.Kinds {
		if slices.Contains(nip29.MetadataEventKinds, kind) {
			isMeta = true
		} else if isMeta {
			// once we have one meta we can't have other stuff
			return true, "it's not allowed to mix metadata kinds with others"
		}
	}

	if !isMeta {
		// we assume the caller wants normal events if the 'h' tag is specified
		// or metadata events if the 'd' tag is specified
		if hTags, hasHTags := filter.Tags["h"]; hasHTags && len(hTags) > 0 {
			isNormal = true
		} else if dTags, hasDTags := filter.Tags["d"]; hasDTags && len(dTags) > 0 {
			isMeta = true
		} else {
			// this may be a request for "#e", "#a" or just "ids"
			isReference = true
		}
	}

	authed := s.GetAuthed(ctx)

	switch {
	case isNormal:
		// access depends on whether a user is logged in and the groups are public or private
		if tags, ok := filter.Tags["h"]; ok && len(tags) > 0 {
			// "h" tags specified
			for _, tag := range tags {
				if group, _ := s.Groups.Load(tag); group != nil {
					if !group.Private {
						continue // fine, this is public
					}

					// private,
					if authed == "" {
						return true, "auth-required: trying to read from a private group"
					}
					// check membership
					group.mu.RLock()
					if _, isMember := group.Members[authed]; isMember {
						group.mu.RUnlock()
						continue // fine, this user is a member
					}
					group.mu.RUnlock()
					return true, "restricted: not a member"
				}
			}
		}
	case isReference:
		if refE, ok := filter.Tags["e"]; ok && len(refE) > 0 {
			// "#e" tags specified -- pablo's magic discovery trick
			// we'll handle this while fetching the events so that we don't reveal private content
			return false, ""
		} else if refA, ok := filter.Tags["a"]; ok && len(refA) > 0 {
			// "#a" tags specified -- idem
			return false, ""
		} else if len(filter.IDs) > 0 {
			// "ids" specified -- idem
			return false, ""
		} else {
			// other tags are not supported (unless they come together with "h")
			return true, "invalid query, must have 'h', 'e' or 'a' tag"
		}
	case isMeta:
		// should be fine
	}

	return false, ""
}
