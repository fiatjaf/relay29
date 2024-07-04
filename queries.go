package main

import (
	"context"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"golang.org/x/exp/slices"
)

func metadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMetadata) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				groups.Range(func(_ string, group *Group) bool {
					if group.Private {
						// don't reveal metadata about private groups in lists
						return true
					} else if group.Closed {
						// closed groups also shouldn't be listed since people can't freely join them
					}

					evt := group.ToMetadataEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
					return true
				})
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := groups.Load(groupId); group != nil {
						evt := group.ToMetadataEvent()
						evt.Sign(s.RelayPrivkey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func adminsQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupAdmins) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				groups.Range(func(_ string, group *Group) bool {
					if group.Private {
						// don't reveal lists of admins of private groups ever
						return true
					}

					evt := group.ToAdminsEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
					return true
				})
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := groups.Load(groupId); group != nil {
						if group.Private {
							// don't reveal lists of admins of private groups ever
							continue
						}
						evt := group.ToAdminsEvent()
						evt.Sign(s.RelayPrivkey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func membersQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMembers) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				groups.Range(func(_ string, group *Group) bool {
					if group.Private {
						// don't reveal lists of members of private groups ever
						return true
					}
					evt := group.ToMembersEvent()
					evt.Sign(s.RelayPrivkey)
					ch <- evt
					return true
				})
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := groups.Load(groupId); group != nil {
						if group.Private {
							// don't reveal lists of members of private groups ever
							continue
						}
						evt := group.ToMembersEvent()
						evt.Sign(s.RelayPrivkey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func normalEventQuery(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	if hTags, hasHTags := filter.Tags["h"]; hasHTags && len(hTags) > 0 {
		// if these tags are present we already know access is safe because we've verified that in filter_policy.go
		return db.QueryEvents(ctx, filter)
	}

	ch := make(chan *nostr.Event)
	authed := khatru.GetAuthed(ctx)
	go func() {
		// now here in refE/refA/ids we have to check for each result if it is allowed
		var results chan *nostr.Event
		var err error
		if refE, ok := filter.Tags["e"]; ok && len(refE) > 0 {
			results, err = db.QueryEvents(ctx, filter)
		} else if refA, ok := filter.Tags["a"]; ok && len(refA) > 0 {
			results, err = db.QueryEvents(ctx, filter)
		} else if len(filter.IDs) > 0 {
			results, err = db.QueryEvents(ctx, filter)
		} else {
			// we must end here for all the metadata queries and so on otherwise they will never close
			close(ch)
		}
		if err != nil {
			return
		}

		allowed := set.NewSliceSet[string]()
		disallowed := set.NewSliceSet[string]()
		for evt := range results {
			if group := getGroupFromEvent(evt); !group.Private || allowed.Has(group.Address.ID) {
				ch <- evt
			} else if authed != "" && !disallowed.Has(group.Address.ID) {
				group.mu.RLock()
				if _, isMember := group.Members[authed]; isMember {
					allowed.Add(group.Address.ID)
					ch <- evt
				} else {
					disallowed.Add(group.Address.ID)
				}
				group.mu.RUnlock()
			}
		}
		close(ch)
	}()

	return ch, nil
}
