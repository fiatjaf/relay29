package relay29

import (
	"context"

	"github.com/fiatjaf/set"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"golang.org/x/exp/slices"
)

func (s *State) MetadataQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	authed := s.GetAuthed(ctx)
	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMetadata) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range s.Groups.Range {
					if group.Private {
						// don't reveal metadata about private groups in lists unless we're a member
						if authed == "" {
							continue
						}
						if _, isMember := group.Members[authed]; !isMember {
							continue
						}
					} else if group.Closed {
						// closed groups also shouldn't be listed since people can't freely join them
						continue
					}

					evt := group.ToMetadataEvent()
					evt.Sign(s.secretKey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := s.Groups.Load(groupId); group != nil {
						evt := group.ToMetadataEvent()
						evt.Sign(s.secretKey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func (s *State) AdminsQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	authed := s.GetAuthed(ctx)
	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupAdmins) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range s.Groups.Range {
					if group.Private {
						// don't reveal lists of admins of private groups unless we're a member
						if authed == "" {
							continue
						}
						if _, isMember := group.Members[authed]; !isMember {
							continue
						}
					}
					if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseAdmins(group.Group, pks) {
						// filter queried p tags
						// TODO
						continue
					}
					evt := group.ToAdminsEvent()
					evt.Sign(s.secretKey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := s.Groups.Load(groupId); group != nil {
						if group.Private {
							// don't reveal lists of admins of private groups unless we're a member
							if authed == "" {
								continue
							}
							if _, isMember := group.Members[authed]; !isMember {
								continue
							}
						}
						if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseAdmins(group.Group, pks) {
							// filter queried p tags
							// TODO
							continue
						}
						evt := group.ToAdminsEvent()
						evt.Sign(s.secretKey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func (s *State) MembersQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	authed := s.GetAuthed(ctx)
	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMembers) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range s.Groups.Range {
					if group.Private {
						// don't reveal lists of members of private groups unless we're a member
						if authed == "" {
							continue
						}
						if _, isMember := group.Members[authed]; !isMember {
							continue
						}
					}
					if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseMembers(group.Group, pks) {
						// filter queried p tags
						// TODO
						continue
					}
					evt := group.ToMembersEvent()
					evt.Sign(s.secretKey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := s.Groups.Load(groupId); group != nil {
						if group.Private {
							// don't reveal lists of members of private groups ever
							if authed == "" {
								continue
							}
							if _, isMember := group.Members[authed]; !isMember {
								continue
							}
						}
						if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseMembers(group.Group, pks) {
							// filter queried p tags
							// TODO
							continue
						}
						evt := group.ToMembersEvent()
						evt.Sign(s.secretKey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func (s *State) RolesQueryHandler(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, 1)

	authed := s.GetAuthed(ctx)
	go func() {
		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupRoles) {
			if _, ok := filter.Tags["d"]; !ok {
				// no "d" tag specified, return everything
				for _, group := range s.Groups.Range {
					if group.Private {
						// don't reveal lists of roles of private groups unless we're a member
						if authed == "" {
							continue
						}
						if _, isMember := group.Members[authed]; !isMember {
							continue
						}
					}
					evt := group.ToRolesEvent()
					evt.Sign(s.secretKey)
					ch <- evt
				}
			} else {
				for _, groupId := range filter.Tags["d"] {
					if group, _ := s.Groups.Load(groupId); group != nil {
						if group.Private {
							// don't reveal lists of members of private groups ever
							if authed == "" {
								continue
							}
							if _, isMember := group.Members[authed]; !isMember {
								continue
							}
						}
						evt := group.ToRolesEvent()
						evt.Sign(s.secretKey)
						ch <- evt
					}
				}
			}
		}

		close(ch)
	}()

	return ch, nil
}

func (s *State) NormalEventQuery(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	if hTags, hasHTags := filter.Tags["h"]; hasHTags && len(hTags) > 0 {
		// if these tags are present we already know access is safe because we've verified that in filter_policy.go
		return s.DB.QueryEvents(ctx, filter)
	}

	ch := make(chan *nostr.Event)
	authed := s.GetAuthed(ctx)
	go func() {
		// now here in refE/refA/ids we have to check for each result if it is allowed
		var results chan *nostr.Event
		var err error
		if refE, ok := filter.Tags["e"]; ok && len(refE) > 0 {
			results, err = s.DB.QueryEvents(ctx, filter)
		} else if refA, ok := filter.Tags["a"]; ok && len(refA) > 0 {
			results, err = s.DB.QueryEvents(ctx, filter)
		} else if len(filter.IDs) > 0 {
			results, err = s.DB.QueryEvents(ctx, filter)
		}

		if err != nil || results == nil {
			// if the previous if didn't catch anything or if we got an error we must end here
			close(ch)
			return
		}

		allowed := set.NewSliceSet[string]()
		disallowed := set.NewSliceSet[string]()
		for evt := range results {
			if group := s.GetGroupFromEvent(evt); !group.Private || allowed.Has(group.Address.ID) {
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

func hasOneOfTheseMembers(group nip29.Group, pubkeys []string) bool {
	for _, pk := range pubkeys {
		if _, ok := group.Members[pk]; ok {
			return true
		}
	}
	return false
}

func hasOneOfTheseAdmins(group nip29.Group, pubkeys []string) bool {
	for _, pk := range pubkeys {
		if role, ok := group.Members[pk]; ok && role != nil {
			return true
		}
	}
	return false
}
