package main

import "github.com/nbd-wtf/go-nostr"

type Action interface {
	Name() string
	Apply(group *Group)
	PermissionRequired() Permission
}

var moderationActionFactories = map[int]func(*nostr.Event) (Action, bool){
	9000: func(evt *nostr.Event) (Action, bool) {
		targets := make([]string, 0, len(evt.Tags))
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKeyHex(tag[1]) {
				return nil, false
			}
			targets = append(targets, tag[1])
		}
		if len(targets) > 0 {
			return &AddUser{Targets: targets}, true
		}
		return nil, false
	},
	9001: func(evt *nostr.Event) (Action, bool) {
		targets := make([]string, 0, len(evt.Tags))
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKeyHex(tag[1]) {
				return nil, false
			}
			targets = append(targets, tag[1])
		}
		if len(targets) > 0 {
			return &RemoveUser{Targets: targets}, true
		}
		return nil, false
	},
	9002: func(evt *nostr.Event) (Action, bool) {
		ok := false
		edit := EditMetadata{}
		if t := evt.Tags.GetFirst([]string{"name", ""}); t != nil {
			edit.NameValue = (*t)[1]
			ok = true
		}
		if t := evt.Tags.GetFirst([]string{"picture", ""}); t != nil {
			edit.PictureValue = (*t)[1]
			ok = true
		}
		if t := evt.Tags.GetFirst([]string{"about", ""}); t != nil {
			edit.AboutValue = (*t)[1]
			ok = true
		}
		if ok {
			return &edit, true
		}
		return nil, false
	},
	9003: func(evt *nostr.Event) (Action, bool) {
		nTags := len(evt.Tags)

		permissions := make([]string, 0, nTags-1)
		for _, tag := range evt.Tags.GetAll([]string{"permission", ""}) {
			if _, ok := availablePermissions[tag[1]]; ok {
				return nil, false
			}
			permissions = append(permissions, tag[1])
		}

		targets := make([]string, 0, nTags-1)
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKeyHex(tag[1]) {
				return nil, false
			}
			targets = append(targets, tag[1])
		}

		if len(permissions) > 0 && len(targets) > 0 {
			return &AddPermission{Targets: targets, Permissions: permissions}, true
		}

		return nil, false
	},
	9004: func(evt *nostr.Event) (Action, bool) {
		nTags := len(evt.Tags)

		permissions := make([]string, 0, nTags-1)
		for _, tag := range evt.Tags.GetAll([]string{"permission", ""}) {
			if _, ok := availablePermissions[tag[1]]; ok {
				return nil, false
			}
			permissions = append(permissions, tag[1])
		}

		targets := make([]string, 0, nTags-1)
		for _, tag := range evt.Tags.GetAll([]string{"p", ""}) {
			if !nostr.IsValidPublicKeyHex(tag[1]) {
				return nil, false
			}
			if tag[1] == s.RelayPubkey {
				continue
			}
			targets = append(targets, tag[1])
		}

		if len(permissions) > 0 && len(targets) > 0 {
			return &RemovePermission{Targets: targets, Permissions: permissions}, true
		}

		return nil, false
	},
	9005: func(evt *nostr.Event) (Action, bool) {
		tags := evt.Tags.GetAll([]string{"e", ""})
		if len(tags) == 0 {
			return nil, false
		}

		targets := make([]string, len(tags))
		for i, tag := range tags {
			if nostr.IsValidPublicKeyHex(tag[1]) {
				targets[i] = tag[1]
			} else {
				return nil, false
			}
		}

		return &DeleteEvent{Targets: targets}, true
	},
	9006: func(evt *nostr.Event) (Action, bool) {
		egs := EditGroupStatus{}

		egs.Public = evt.Tags.GetFirst([]string{"public"}) != nil
		egs.Private = evt.Tags.GetFirst([]string{"private"}) != nil
		egs.Open = evt.Tags.GetFirst([]string{"open"}) != nil
		egs.Closed = evt.Tags.GetFirst([]string{"closed"}) != nil

		// disallow contradictions
		if egs.Public && egs.Private {
			return nil, false
		}
		if egs.Open && egs.Closed {
			return nil, false
		}

		// TODO remove this once we start supporting private groups
		if egs.Private {
			return nil, false
		}

		return egs, true
	},
}

type DeleteEvent struct {
	Targets []string
}

func (DeleteEvent) Name() string                   { return "delete-event" }
func (DeleteEvent) PermissionRequired() Permission { return PermDeleteEvent }
func (a DeleteEvent) Apply(group *Group)           {}

type AddUser struct {
	Targets []string
}

func (AddUser) Name() string                   { return "add-user" }
func (AddUser) PermissionRequired() Permission { return PermAddUser }
func (a AddUser) Apply(group *Group) {
	for _, target := range a.Targets {
		group.Members[target] = emptyRole
	}
}

type RemoveUser struct {
	Targets []string
}

func (RemoveUser) Name() string                   { return "remove-user" }
func (RemoveUser) PermissionRequired() Permission { return PermRemoveUser }
func (a RemoveUser) Apply(group *Group) {
	for _, target := range a.Targets {
		if target == s.RelayPubkey {
			continue
		}
		delete(group.Members, target)
	}
}

type EditMetadata struct {
	NameValue    string
	PictureValue string
	AboutValue   string
}

func (EditMetadata) Name() string                   { return "edit-metadata" }
func (EditMetadata) PermissionRequired() Permission { return PermEditMetadata }
func (a EditMetadata) Apply(group *Group) {
	group.Name = a.NameValue
	group.Picture = a.PictureValue
	group.About = a.AboutValue
}

type AddPermission struct {
	Targets     []string
	Permissions []Permission
}

func (AddPermission) Name() string                   { return "add-permission" }
func (AddPermission) PermissionRequired() Permission { return PermAddPermission }
func (a AddPermission) Apply(group *Group) {
	for _, target := range a.Targets {
		role, ok := group.Members[target]

		// if it's a normal user, create a new permissions object thing for this user
		// instead of modifying the global emptyRole
		if !ok || role == emptyRole {
			role = &Role{Permissions: make(map[string]struct{})}
			group.Members[target] = role
		}

		// add all permissions listed
		for _, perm := range a.Permissions {
			role.Permissions[perm] = struct{}{}
		}
	}
}

type RemovePermission struct {
	Targets     []string
	Permissions []Permission
}

func (RemovePermission) Name() string                   { return "remove-permission" }
func (RemovePermission) PermissionRequired() Permission { return PermRemovePermission }
func (a RemovePermission) Apply(group *Group) {
	for _, target := range a.Targets {
		if target == s.RelayPubkey {
			continue
		}

		role, ok := group.Members[target]
		if !ok || role == emptyRole {
			continue
		}

		// remove all permissions listed
		for _, perm := range a.Permissions {
			delete(role.Permissions, perm)
		}

		// if no more permissions are available, change this guy to be a normal user
		if role.Name == "" && len(role.Permissions) == 0 {
			group.Members[target] = emptyRole
		}
	}
}

type EditGroupStatus struct {
	Public  bool
	Private bool
	Open    bool
	Closed  bool
}

func (EditGroupStatus) Name() string                   { return "edit-group-status" }
func (EditGroupStatus) PermissionRequired() Permission { return PermEditGroupStatus }
func (a EditGroupStatus) Apply(group *Group) {
	if a.Public {
		group.Private = false
	} else if a.Private {
		group.Private = true
	}

	if a.Open {
		group.Closed = false
	} else if a.Closed {
		group.Closed = true
	}
}
