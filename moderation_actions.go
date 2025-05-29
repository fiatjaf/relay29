package relay29

import (
	"fmt"
	"slices"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
)

var PTagNotValidPublicKey = fmt.Errorf("'p' tag value is not a valid public key")

type Action interface {
	Apply(group *nip29.Group)
	Name() string
}

var (
	_ Action = PutUser{}
	_ Action = RemoveUser{}
	_ Action = CreateGroup{}
	_ Action = DeleteEvent{}
	_ Action = EditMetadata{}
)

func PrepareModerationAction(evt *nostr.Event) (Action, error) {
	factory, ok := moderationActionFactories[evt.Kind]
	if !ok {
		return nil, fmt.Errorf("event kind %d is not a supported moderation action", evt.Kind)
	}
	return factory(evt)
}

var moderationActionFactories = map[int]func(*nostr.Event) (Action, error){
	nostr.KindSimpleGroupPutUser: func(evt *nostr.Event) (Action, error) {
		targets := make([]PubKeyRoles, 0, len(evt.Tags))
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKey(tag[1]) {
				return nil, PTagNotValidPublicKey
			}
			targets = append(targets, PubKeyRoles{
				PubKey:    tag[1],
				RoleNames: tag[2:],
			})
		}
		if len(targets) > 0 {
			return PutUser{Targets: targets, When: evt.CreatedAt}, nil
		}
		return nil, fmt.Errorf("missing 'p' tags")
	},
	nostr.KindSimpleGroupRemoveUser: func(evt *nostr.Event) (Action, error) {
		targets := make([]string, 0, len(evt.Tags))
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKey(tag[1]) {
				return nil, PTagNotValidPublicKey
			}
			targets = append(targets, tag[1])
		}
		if len(targets) > 0 {
			return RemoveUser{Targets: targets, When: evt.CreatedAt}, nil
		}
		return nil, fmt.Errorf("missing 'p' tags")
	},
	nostr.KindSimpleGroupEditMetadata: func(evt *nostr.Event) (Action, error) {
		ok := false
		edit := EditMetadata{When: evt.CreatedAt}
		if t := evt.Tags.GetFirst([]string{"name", ""}); t != nil {
			edit.NameValue = &(*t)[1]
			ok = true
		}
		if t := evt.Tags.GetFirst([]string{"picture", ""}); t != nil {
			edit.PictureValue = &(*t)[1]
			ok = true
		}
		if t := evt.Tags.GetFirst([]string{"about", ""}); t != nil {
			edit.AboutValue = &(*t)[1]
			ok = true
		}

		y := true
		n := false

		if t := evt.Tags.GetFirst([]string{"public"}); t != nil {
			edit.PrivateValue = &n
			ok = true
		} else if t := evt.Tags.GetFirst([]string{"private"}); t != nil {
			edit.PrivateValue = &y
			ok = true
		}

		if t := evt.Tags.GetFirst([]string{"open"}); t != nil {
			edit.ClosedValue = &n
			ok = true
		} else if t := evt.Tags.GetFirst([]string{"closed"}); t != nil {
			edit.ClosedValue = &y
			ok = true
		}

		if ok {
			return edit, nil
		}
		return nil, fmt.Errorf("missing metadata tags")
	},
	nostr.KindSimpleGroupDeleteEvent: func(evt *nostr.Event) (Action, error) {
		tags := evt.Tags.GetAll([]string{"e", ""})
		if len(tags) == 0 {
			return nil, fmt.Errorf("missing 'e' tag")
		}

		targets := make([]string, len(tags))
		for i, tag := range tags {
			if nostr.IsValid32ByteHex(tag[1]) {
				targets[i] = tag[1]
			} else {
				return nil, fmt.Errorf("invalid event id hex")
			}
		}

		return DeleteEvent{Targets: targets}, nil
	},
	nostr.KindSimpleGroupCreateGroup: func(evt *nostr.Event) (Action, error) {
		return CreateGroup{Creator: evt.PubKey, When: evt.CreatedAt}, nil
	},
	nostr.KindSimpleGroupDeleteGroup: func(evt *nostr.Event) (Action, error) {
		return DeleteGroup{When: evt.CreatedAt}, nil
	},
}

type DeleteEvent struct {
	Targets []string
}

func (_ DeleteEvent) Name() string             { return "delete-event" }
func (a DeleteEvent) Apply(group *nip29.Group) {}

type PubKeyRoles struct {
	PubKey    string
	RoleNames []string
}

type PutUser struct {
	Targets []PubKeyRoles
	When    nostr.Timestamp
}

func (_ PutUser) Name() string { return "put-user" }
func (a PutUser) Apply(group *nip29.Group) {
	for _, target := range a.Targets {
		roles := make([]*nip29.Role, 0, len(target.RoleNames))
		for _, roleName := range target.RoleNames {
			if slices.IndexFunc(roles, func(r *nip29.Role) bool { return r.Name == roleName }) != -1 {
				continue
			}
			roles = append(roles, group.GetRoleByName(roleName))
		}
		group.Members[target.PubKey] = roles
	}
}

type RemoveUser struct {
	Targets []string
	When    nostr.Timestamp
}

func (_ RemoveUser) Name() string { return "remove-user" }
func (a RemoveUser) Apply(group *nip29.Group) {
	for _, tpk := range a.Targets {
		delete(group.Members, tpk)
	}
}

type EditMetadata struct {
	NameValue    *string
	PictureValue *string
	AboutValue   *string
	PrivateValue *bool
	ClosedValue  *bool
	When         nostr.Timestamp
}

func (_ EditMetadata) Name() string { return "edit-metadata" }
func (a EditMetadata) Apply(group *nip29.Group) {
	group.LastMetadataUpdate = a.When
	if a.NameValue != nil {
		group.Name = *a.NameValue
	}
	if a.PictureValue != nil {
		group.Picture = *a.PictureValue
	}
	if a.AboutValue != nil {
		group.About = *a.AboutValue
	}
	if a.PrivateValue != nil {
		group.Private = *a.PrivateValue
	}
	if a.ClosedValue != nil {
		group.Closed = *a.ClosedValue
	}
}

type CreateGroup struct {
	Creator string
	When    nostr.Timestamp
}

func (_ CreateGroup) Name() string { return "create-group" }
func (a CreateGroup) Apply(group *nip29.Group) {
	group.LastMetadataUpdate = a.When
	group.LastAdminsUpdate = a.When
	group.LastMembersUpdate = a.When
}

type DeleteGroup struct {
	When nostr.Timestamp
}

func (_ DeleteGroup) Name() string { return "delete-group" }
func (a DeleteGroup) Apply(group *nip29.Group) {
	group.Members = make(map[string][]*nip29.Role)
	group.Closed = true
	group.Private = true
	group.Name = "[deleted]"
	group.About = ""
	group.Picture = ""
	group.LastMetadataUpdate = a.When
	group.LastAdminsUpdate = a.When
	group.LastMembersUpdate = a.When
}
