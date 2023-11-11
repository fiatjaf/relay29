package main

import "github.com/nbd-wtf/go-nostr"

type Action interface {
	Name() string
	Apply(group *Group)
	PermissionRequired() Permission
}

var moderationActionFactories = map[int]func(*nostr.Event) (Action, bool){
	9000: func(evt *nostr.Event) (Action, bool) {
		if tag := evt.Tags.GetFirst([]string{"p", ""}); tag != nil {
			if nostr.IsValidPublicKeyHex((*tag)[1]) {
				return &AddUser{
					Target: (*tag)[1],
				}, true
			}
		}
		return nil, false
	},
	9001: func(evt *nostr.Event) (Action, bool) {
		if tag := evt.Tags.GetFirst([]string{"p", ""}); tag != nil {
			if nostr.IsValidPublicKeyHex((*tag)[1]) {
				return &RemoveUser{
					Target: (*tag)[1],
				}, true
			}
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
		var perm Permission
		if tag := evt.Tags.GetFirst([]string{"permission", ""}); tag != nil {
			perm = (*tag)[1]
		}
		if _, ok := availablePermissions[perm]; !ok {
			return nil, false
		}
		if tag := evt.Tags.GetFirst([]string{"p", ""}); tag != nil {
			if nostr.IsValidPublicKeyHex((*tag)[1]) {
				return &AddPermission{
					Target:     (*tag)[1],
					Permission: perm,
				}, true
			}
		}
		return nil, false
	},
	9004: func(evt *nostr.Event) (Action, bool) {
		var perm Permission
		if tag := evt.Tags.GetFirst([]string{"permission", ""}); tag != nil {
			perm = (*tag)[1]
		}
		if _, ok := availablePermissions[perm]; !ok {
			return nil, false
		}
		if tag := evt.Tags.GetFirst([]string{"p", ""}); tag != nil {
			if nostr.IsValidPublicKeyHex((*tag)[1]) {
				return &RemovePermission{
					Target:     (*tag)[1],
					Permission: perm,
				}, true
			}
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

		return &DeleteEvent{
			Targets: targets,
		}, true
	},
}

type DeleteEvent struct {
	Targets []string
}

func (DeleteEvent) Name() string                   { return "delete-event" }
func (DeleteEvent) PermissionRequired() Permission { return PermDeleteEvent }
func (a DeleteEvent) Apply(group *Group)           {}

type AddUser struct {
	Target string
}

func (AddUser) Name() string                   { return "add-user" }
func (AddUser) PermissionRequired() Permission { return PermAddUser }
func (a AddUser) Apply(group *Group) {
	group.Members[a.Target] = emptyRole
}

type RemoveUser struct {
	Target string
}

func (RemoveUser) Name() string                   { return "remove-user" }
func (RemoveUser) PermissionRequired() Permission { return PermRemoveUser }
func (a RemoveUser) Apply(group *Group) {
	delete(group.Members, a.Target)
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
	Target     string
	Permission Permission
}

func (AddPermission) Name() string                   { return "add-permission" }
func (AddPermission) PermissionRequired() Permission { return PermAddPermission }
func (a AddPermission) Apply(group *Group) {
	role, ok := group.Members[a.Target]
	if !ok || role == emptyRole {
		role = &Role{Permissions: make(map[string]struct{})}
		group.Members[a.Target] = role
	}
	role.Permissions[a.Permission] = struct{}{}
}

type RemovePermission struct {
	Target     string
	Permission Permission
}

func (RemovePermission) Name() string                   { return "remove-permission" }
func (RemovePermission) PermissionRequired() Permission { return PermRemovePermission }
func (a RemovePermission) Apply(group *Group) {
	role, ok := group.Members[a.Target]
	if ok && role != emptyRole {
		delete(role.Permissions, a.Permission)
		if role.Name == "" && len(role.Permissions) == 0 {
			group.Members[a.Target] = emptyRole
		}
	}
}
