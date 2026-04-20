package khatru29

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/stretchr/testify/require"
)

// tickTimestamp returns a strictly monotonic timestamp (seconds) so successive
// replaceable events (kind:39000) never collide on (kind,pubkey,d,content,tags)
// and get deduped by the client.
var tsCounter atomic.Int64

func tickTimestamp() nostr.Timestamp {
	return nostr.Timestamp(nostr.Now()) + nostr.Timestamp(tsCounter.Add(1))
}

// startSubgroupRelay spins up a throwaway khatru29 relay on an ephemeral port
// and returns its ws:// URL plus a shutdown function. It drops the
// "moderation-events-must-be-recent" policy so tests can control timestamps.
func startSubgroupRelay(t *testing.T) (string, func()) {
	t.Helper()
	db := &slicestore.SliceStore{}
	db.Init()
	url, shutdown, _ := startSubgroupRelayWithDB(t, db, nostr.GeneratePrivateKey())
	return url, shutdown
}

// startSubgroupRelayWithDB starts a relay backed by the given DB + relay secret
// key, so callers can simulate a restart by reusing the same DB.
func startSubgroupRelayWithDB(t *testing.T, db *slicestore.SliceStore, relaySk string) (string, func(), *relay29.State) {
	t.Helper()
	relay, state := Init(relay29.Options{
		Domain:                  "localhost",
		DB:                      db,
		SecretKey:               relaySk,
		DefaultRoles:            []*nip29.Role{ceo, secretary},
		GroupCreatorDefaultRole: ceo,
	})

	state.AllowAction = func(ctx context.Context, group nip29.Group, role *nip29.Role, action relay29.Action) bool {
		return role == ceo
	}

	relay.RejectEvent = slices.DeleteFunc(relay.RejectEvent, func(f func(ctx context.Context, event *nostr.Event) (reject bool, msg string)) bool {
		return fmt.Sprintf("%v", []any{f}) == fmt.Sprintf("%v", []any{state.RequireModerationEventsToBeRecent})
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: relay}
	go server.Serve(ln)

	url := "ws://" + ln.Addr().String()
	return url, func() { server.Shutdown(context.Background()) }, state
}

// waitForMetadata drains a subscription until it sees a kind:39000 for the
// given group id matching `match`, or fails the test after 2s.
func waitForMetadata(t *testing.T, sub *nostr.Subscription, groupId string, match func(*nostr.Event) bool) *nostr.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-sub.Events:
			if evt == nil {
				continue
			}
			if evt.Tags.GetD() != groupId {
				continue
			}
			if match(evt) {
				return evt
			}
		case <-deadline:
			t.Fatalf("timed out waiting for kind:39000 of %q", groupId)
			return nil
		}
	}
}

func createGroup(t *testing.T, ctx context.Context, r *nostr.Relay, sk, id string) {
	t.Helper()
	e := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9007, Tags: nostr.Tags{{"h", id}}}
	require.NoError(t, e.Sign(sk))
	require.NoError(t, r.Publish(ctx, e), "create-group %q", id)
}

func editParent(t *testing.T, ctx context.Context, r *nostr.Relay, sk, id string, parent ...string) error {
	t.Helper()
	tag := nostr.Tag{"parent"}
	tag = append(tag, parent...)
	e := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9002, Tags: nostr.Tags{{"h", id}, tag}}
	require.NoError(t, e.Sign(sk))
	return r.Publish(ctx, e)
}

func TestSubgroupAttachAndDetach(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")
	createGroup(t, ctx, r, sk, "nostr")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"nostr"}}}})
	require.NoError(t, err)

	// attach nostr under tech
	require.NoError(t, editParent(t, ctx, r, sk, "nostr", "tech"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "tech"
	})

	// detach via ["parent"] (no value)
	require.NoError(t, editParent(t, ctx, r, sk, "nostr"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"parent"}) == nil
	})

	// attach again, then detach via ["parent", ""]
	require.NoError(t, editParent(t, ctx, r, sk, "nostr", "tech"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "tech"
	})
	require.NoError(t, editParent(t, ctx, r, sk, "nostr", ""))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"parent"}) == nil
	})
}

func TestSubgroupReparent(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	for _, id := range []string{"tech", "social", "nostr"} {
		createGroup(t, ctx, r, sk, id)
	}

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"nostr"}}}})
	require.NoError(t, err)

	require.NoError(t, editParent(t, ctx, r, sk, "nostr", "tech"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "tech"
	})

	require.NoError(t, editParent(t, ctx, r, sk, "nostr", "social"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "social"
	})
}

func TestSubgroupCycleRejected(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	// build chain: a -> b -> c (c is root, b under c, a under b)
	for _, id := range []string{"a", "b", "c"} {
		createGroup(t, ctx, r, sk, id)
	}
	require.NoError(t, editParent(t, ctx, r, sk, "b", "c"))
	require.NoError(t, editParent(t, ctx, r, sk, "a", "b"))

	// self-reference
	require.Error(t, editParent(t, ctx, r, sk, "a", "a"), "self-parent must be rejected")

	// multi-hop: try to put c under a (would form c->a->b->c)
	require.Error(t, editParent(t, ctx, r, sk, "c", "a"), "multi-hop cycle must be rejected")
}

func TestSubgroupMissingParentAccepted(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "orphan")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"orphan"}}}})
	require.NoError(t, err)

	// per spec: a declared parent that does not exist is accepted; the
	// subgroup is simply treated as a root by clients building the tree.
	require.NoError(t, editParent(t, ctx, r, sk, "orphan", "ghost"))

	waitForMetadata(t, sub, "orphan", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "ghost"
	})
}

func TestSubgroupDeletePromotesChildrenToRoots(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	for _, id := range []string{"parent", "child1", "child2"} {
		createGroup(t, ctx, r, sk, id)
	}
	require.NoError(t, editParent(t, ctx, r, sk, "child1", "parent"))
	require.NoError(t, editParent(t, ctx, r, sk, "child2", "parent"))

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"child1", "child2"}}}})
	require.NoError(t, err)

	// delete parent
	del := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9008, Tags: nostr.Tags{{"h", "parent"}}}
	require.NoError(t, del.Sign(sk))
	require.NoError(t, r.Publish(ctx, del))

	// both children should now broadcast kind:39000 without a parent tag
	promoted := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(promoted) < 2 {
		select {
		case evt := <-sub.Events:
			if evt == nil {
				continue
			}
			d := evt.Tags.GetD()
			if d != "child1" && d != "child2" {
				continue
			}
			if evt.Tags.GetFirst([]string{"parent"}) == nil {
				promoted[d] = true
			}
		case <-deadline:
			t.Fatalf("only saw %v promoted to root", promoted)
		}
	}
}

func TestSubgroupParentAttestation(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()
	adminPk, _ := nostr.GetPublicKey(sk)

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")
	createGroup(t, ctx, r, sk, "nostr")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"nostr"}}}})
	require.NoError(t, err)

	// attach → third element on parent tag should be the authoring admin's pubkey
	require.NoError(t, editParent(t, ctx, r, sk, "nostr", "tech"))
	evt := waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 3 && (*pt)[1] == "tech"
	})
	pt := evt.Tags.GetFirst([]string{"parent"})
	require.Equal(t, adminPk, (*pt)[2], "attester pubkey must match event.PubKey")

	// detach → attester must be cleared (tag gone entirely)
	require.NoError(t, editParent(t, ctx, r, sk, "nostr"))
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"parent"}) == nil
	})
}

func TestSubgroupChildEntriesAndClosedChildren(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech"}}}})
	require.NoError(t, err)

	// parent admin declares a bilateral acceptance list with order + flags,
	// plus the closed-children flag.
	e := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags: nostr.Tags{
			{"h", "tech"},
			{"child", "nostr", "a", "suggested"},
			{"child", "music", "b"},
			{"closed-children"},
		},
	}
	require.NoError(t, e.Sign(sk))
	require.NoError(t, r.Publish(ctx, e))

	evt := waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"closed-children"}) != nil
	})

	children := evt.Tags.GetAll([]string{"child"})
	require.Len(t, children, 2)
	require.Equal(t, "nostr", children[0][1])
	require.Equal(t, "a", children[0][2])
	require.Equal(t, "suggested", children[0][3])
	require.Equal(t, "music", children[1][1])
	require.Equal(t, "b", children[1][2])
	require.NotNil(t, evt.Tags.GetFirst([]string{"closed-children"}))
}

func TestSubgroupReparentUpdatesAttester(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()

	// two distinct admins. both are given the ceo role on the child so
	// either can reparent it.
	sk1 := nostr.GeneratePrivateKey()
	pk1, _ := nostr.GetPublicKey(sk1)
	sk2 := nostr.GeneratePrivateKey()
	pk2, _ := nostr.GetPublicKey(sk2)

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk1, "tech")
	createGroup(t, ctx, r, sk1, "social")
	createGroup(t, ctx, r, sk1, "nostr")

	// promote pk2 to ceo of nostr so it can reparent
	put := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9000,
		Tags:      nostr.Tags{{"h", "nostr"}, {"p", pk2, "ceo"}},
	}
	require.NoError(t, put.Sign(sk1))
	require.NoError(t, r.Publish(ctx, put))

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"nostr"}}}})
	require.NoError(t, err)

	// sk1 attaches nostr under tech → attester = pk1
	require.NoError(t, editParent(t, ctx, r, sk1, "nostr", "tech"))
	evt := waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 3 && (*pt)[1] == "tech"
	})
	require.Equal(t, pk1, (*evt.Tags.GetFirst([]string{"parent"}))[2])

	// sk2 reparents nostr under social → attester updates to pk2
	require.NoError(t, editParent(t, ctx, r, sk2, "nostr", "social"))
	evt = waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "social"
	})
	require.Equal(t, pk2, (*evt.Tags.GetFirst([]string{"parent"}))[2])
}

func TestSubgroupIndependentMembership(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()
	member := nostr.GeneratePrivateKey()
	memberPk, _ := nostr.GetPublicKey(member)

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "parent")
	createGroup(t, ctx, r, sk, "child")
	require.NoError(t, editParent(t, ctx, r, sk, "child", "parent"))

	// add `member` only to `parent`
	inv := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9000, Tags: nostr.Tags{{"h", "parent"}, {"p", memberPk}}}
	require.NoError(t, inv.Sign(sk))
	require.NoError(t, r.Publish(ctx, inv))

	// parent membership allows writing in parent
	writeToParent := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9, Content: "hi", Tags: nostr.Tags{{"h", "parent"}}}
	require.NoError(t, writeToParent.Sign(member))
	require.NoError(t, r.Publish(ctx, writeToParent), "member of parent must write in parent")

	// but NOT in child (independent membership)
	writeToChild := nostr.Event{CreatedAt: tickTimestamp(), Kind: 9, Content: "hi", Tags: nostr.Tags{{"h", "child"}}}
	require.NoError(t, writeToChild.Sign(member))
	require.Error(t, r.Publish(ctx, writeToChild), "parent membership must not grant child write access")
}

// TestSubgroupOpenChildrenUnsetsFlag verifies that ["open-children"] clears
// a previously-set closed-children flag — the symmetric counterpart, following
// the same pattern used for public/private and open/closed.
func TestSubgroupOpenChildrenUnsetsFlag(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech"}}}})
	require.NoError(t, err)

	// set closed-children
	e1 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"closed-children"}},
	}
	require.NoError(t, e1.Sign(sk))
	require.NoError(t, r.Publish(ctx, e1))
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"closed-children"}) != nil
	})

	// unset via open-children
	e2 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"open-children"}},
	}
	require.NoError(t, e2.Sign(sk))
	require.NoError(t, r.Publish(ctx, e2), "open-children must be accepted")
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		// accept the event where closed-children is gone and open-children is
		// not re-emitted (it's not a state we advertise, just a signal).
		return evt.Tags.GetFirst([]string{"closed-children"}) == nil
	})
}

// TestSubgroupClearChildList verifies that sending a bare ["child"] tag (no
// id) on a kind:9002 clears the bilateral acceptance list.
func TestSubgroupClearChildList(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech"}}}})
	require.NoError(t, err)

	// set a child list
	e1 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"child", "a"}, {"child", "b"}},
	}
	require.NoError(t, e1.Sign(sk))
	require.NoError(t, r.Publish(ctx, e1))
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return len(evt.Tags.GetAll([]string{"child"})) == 2
	})

	// clear via a bare ["child"] sentinel
	e2 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"child"}},
	}
	require.NoError(t, e2.Sign(sk))
	require.NoError(t, r.Publish(ctx, e2), "bare child tag must be accepted")
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return len(evt.Tags.GetAll([]string{"child"})) == 0
	})
}

// TestSubgroupPersistenceAcrossRestart exercises the loadGroupsFromDB second
// pass: after publishing subgroup-related kind:9002 events, shut the relay
// down, start a new one backed by the same DB, and verify every subgroup
// field (parent, attester, child list, closed-children) was rebuilt.
func TestSubgroupPersistenceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	db := &slicestore.SliceStore{}
	db.Init()
	relaySk := nostr.GeneratePrivateKey()
	adminSk := nostr.GeneratePrivateKey()
	adminPk, _ := nostr.GetPublicKey(adminSk)

	url, shutdown, _ := startSubgroupRelayWithDB(t, db, relaySk)

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	for _, id := range []string{"tech", "nostr"} {
		createGroup(t, ctx, r, adminSk, id)
	}
	require.NoError(t, editParent(t, ctx, r, adminSk, "nostr", "tech"))

	// declare bilateral children + closed-children on the parent
	e := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags: nostr.Tags{
			{"h", "tech"},
			{"child", "nostr", "a"},
			{"closed-children"},
		},
	}
	require.NoError(t, e.Sign(adminSk))
	require.NoError(t, r.Publish(ctx, e))

	// sanity-check the state before restart
	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech", "nostr"}}}})
	require.NoError(t, err)
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"closed-children"}) != nil
	})
	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 3 && (*pt)[1] == "tech" && (*pt)[2] == adminPk
	})
	r.Close()
	shutdown()

	// restart on the same DB
	url2, shutdown2, _ := startSubgroupRelayWithDB(t, db, relaySk)
	defer shutdown2()

	r2, err := nostr.RelayConnect(ctx, url2)
	require.NoError(t, err)

	sub2, err := r2.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech", "nostr"}}}})
	require.NoError(t, err)

	// drain both events (the query handler emits them in the order filter.Tags["d"]
	// is iterated, which on a multi-d subscription can interleave responses with
	// pre-read events); collect until we've seen both groups.
	var techEvt, nostrEvt *nostr.Event
	deadline := time.After(2 * time.Second)
	for techEvt == nil || nostrEvt == nil {
		select {
		case evt := <-sub2.Events:
			if evt == nil {
				continue
			}
			switch evt.Tags.GetD() {
			case "tech":
				if techEvt == nil {
					techEvt = evt
				}
			case "nostr":
				if nostrEvt == nil {
					nostrEvt = evt
				}
			}
		case <-deadline:
			t.Fatalf("timed out — techEvt=%v nostrEvt=%v", techEvt != nil, nostrEvt != nil)
		}
	}

	// parent link + attester pubkey survived
	pt := nostrEvt.Tags.GetFirst([]string{"parent"})
	require.NotNil(t, pt)
	require.GreaterOrEqual(t, len(*pt), 3)
	require.Equal(t, "tech", (*pt)[1])
	require.Equal(t, adminPk, (*pt)[2])

	// child list + closed-children flag survived
	require.NotNil(t, techEvt.Tags.GetFirst([]string{"closed-children"}))
	children := techEvt.Tags.GetAll([]string{"child"})
	require.Len(t, children, 1)
	require.Equal(t, "nostr", children[0][1])
	require.Equal(t, "a", children[0][2])
}

// TestSubgroupReplayRejectsCycles guards against a rare pre-save/post-save
// race in which two concurrent reparents both pass the pre-save cycle check,
// are both stored in the DB, but only one wins the reparent mutex at runtime
// (the loser is refused in memory but its kind:9002 persists). On restart the
// second-pass replay must skip the cycle-inducing event instead of blindly
// reapplying it.
//
// We simulate this by seeding the DB directly with a chain a<-b<-c plus a
// rogue kind:9002 that would reparent c under a (forming c->a->b->c), then
// boot a relay on it and verify the in-memory tree is acyclic.
func TestSubgroupReplayRejectsCycles(t *testing.T) {
	ctx := context.Background()
	db := &slicestore.SliceStore{}
	db.Init()
	relaySk := nostr.GeneratePrivateKey()
	adminSk := nostr.GeneratePrivateKey()
	adminPk, _ := nostr.GetPublicKey(adminSk)

	mk := func(kind int, tags nostr.Tags) *nostr.Event {
		e := &nostr.Event{CreatedAt: tickTimestamp(), Kind: kind, Tags: tags, PubKey: adminPk}
		require.NoError(t, e.Sign(adminSk))
		return e
	}

	// seed: create-group a, b, c
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, db.SaveEvent(ctx, mk(9007, nostr.Tags{{"h", id}})))
	}
	// b -> c
	require.NoError(t, db.SaveEvent(ctx, mk(9002, nostr.Tags{{"h", "b"}, {"parent", "c"}})))
	// a -> b
	require.NoError(t, db.SaveEvent(ctx, mk(9002, nostr.Tags{{"h", "a"}, {"parent", "b"}})))
	// rogue: c -> a (cycle c->a->b->c). This is what would survive a
	// concurrent-reparent race.
	require.NoError(t, db.SaveEvent(ctx, mk(9002, nostr.Tags{{"h", "c"}, {"parent", "a"}})))

	url, shutdown, state := startSubgroupRelayWithDB(t, db, relaySk)
	defer shutdown()

	// in-memory state must be acyclic: c stays root (rogue skipped)
	a, _ := state.Groups.Load("a")
	b, _ := state.Groups.Load("b")
	c, _ := state.Groups.Load("c")
	require.NotNil(t, a)
	require.NotNil(t, b)
	require.NotNil(t, c)
	require.Equal(t, "b", a.Parent, "a's parent survives")
	require.Equal(t, "c", b.Parent, "b's parent survives")
	require.Equal(t, "", c.Parent, "cycle-inducing parent must be skipped")

	// and the emitted kind:39000 for c must not carry a parent tag
	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)
	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"c"}}}})
	require.NoError(t, err)
	waitForMetadata(t, sub, "c", func(evt *nostr.Event) bool {
		return evt.Tags.GetFirst([]string{"parent"}) == nil
	})
}

// TestSubgroupChildListReplacement verifies that a new kind:9002 carrying
// `child` tags replaces the parent's entire acceptance list rather than
// appending.
func TestSubgroupChildListReplacement(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"tech"}}}})
	require.NoError(t, err)

	// first: two children
	e1 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"child", "a"}, {"child", "b"}},
	}
	require.NoError(t, e1.Sign(sk))
	require.NoError(t, r.Publish(ctx, e1))
	waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		return len(evt.Tags.GetAll([]string{"child"})) == 2
	})

	// second: only one child — list should be replaced, not appended
	e2 := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags:      nostr.Tags{{"h", "tech"}, {"child", "c"}},
	}
	require.NoError(t, e2.Sign(sk))
	require.NoError(t, r.Publish(ctx, e2))

	evt := waitForMetadata(t, sub, "tech", func(evt *nostr.Event) bool {
		children := evt.Tags.GetAll([]string{"child"})
		return len(children) == 1 && children[0][1] == "c"
	})
	require.Len(t, evt.Tags.GetAll([]string{"child"}), 1, "child list must be replaced, not accumulated")
}

// TestSubgroupUnknownTagsIgnored verifies the spec "Relays SHOULD ignore
// unknown tags rather than reject the event": a kind:9002 carrying both a
// valid parent tag and an unknown tag is accepted and the parent is applied.
func TestSubgroupUnknownTagsIgnored(t *testing.T) {
	url, shutdown := startSubgroupRelay(t)
	defer shutdown()

	ctx := context.Background()
	sk := nostr.GeneratePrivateKey()

	r, err := nostr.RelayConnect(ctx, url)
	require.NoError(t, err)

	createGroup(t, ctx, r, sk, "tech")
	createGroup(t, ctx, r, sk, "nostr")

	sub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"nostr"}}}})
	require.NoError(t, err)

	e := nostr.Event{
		CreatedAt: tickTimestamp(),
		Kind:      9002,
		Tags: nostr.Tags{
			{"h", "nostr"},
			{"parent", "tech"},
			{"totally-made-up-future-tag", "x", "y"},
		},
	}
	require.NoError(t, e.Sign(sk))
	require.NoError(t, r.Publish(ctx, e), "unknown tags must not cause rejection")

	waitForMetadata(t, sub, "nostr", func(evt *nostr.Event) bool {
		pt := evt.Tags.GetFirst([]string{"parent"})
		return pt != nil && len(*pt) >= 2 && (*pt)[1] == "tech"
	})
}
