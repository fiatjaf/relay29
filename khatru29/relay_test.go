package khatru29

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore/slicestore"
	"github.com/fiatjaf/relay29"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

var relayPrivateKey = nostr.GeneratePrivateKey()

var (
	ceo       = &nip29.Role{Name: "ceo", Description: "the boss"}
	secretary = &nip29.Role{Name: "secretary", Description: "the actual boss"}
)

func startTestRelay() func() {
	db := &slicestore.SliceStore{}
	db.Init()

	relay, state := Init(relay29.Options{
		Domain:                  "localhost:29292",
		DB:                      db,
		SecretKey:               relayPrivateKey,
		DefaultRoles:            []*nip29.Role{ceo, secretary},
		GroupCreatorDefaultRole: ceo,
	})

	state.AllowAction = func(ctx context.Context, group nip29.Group, role *nip29.Role, action relay29.Action) bool {
		if role == ceo {
			if _, ok := action.(relay29.DeleteEvent); ok {
				return false
			}
			return true
		}
		if role == secretary {
			if _, ok := action.(relay29.EditMetadata); ok {
				return false
			}
			return true
		}
		return false
	}

	relay.Info.Name = "very testy relay"
	relay.Info.Description = "this is just for testing"

	// don't do this at home -- we're going to remove one requirement to make tests simpler
	relay.RejectEvent = slices.DeleteFunc(relay.RejectEvent, func(f func(ctx context.Context, event *nostr.Event) (reject bool, msg string)) bool {
		return fmt.Sprintf("%v", []any{f}) == fmt.Sprintf("%v", []any{state.RequireModerationEventsToBeRecent})
	})

	server := &http.Server{Addr: ":29292", Handler: relay}

	go func() {
		server.ListenAndServe()
	}()

	return func() {
		server.Shutdown(context.Background())
	}
}

func TestGroupStuffABunch(t *testing.T) {
	defer startTestRelay()()
	ctx := context.Background()

	user1 := "0000000000000000000000000000000000000000000000000000000000000001"
	user1pk, _ := nostr.GetPublicKey(user1)

	user2 := "0000000000000000000000000000000000000000000000000000000000000002"
	user2pk, _ := nostr.GetPublicKey(user2)

	user3 := "0000000000000000000000000000000000000000000000000000000000000003"
	user3pk, _ := nostr.GetPublicKey(user3)

	// simple open group
	{
		r, err := nostr.RelayConnect(ctx, "ws://localhost:29292")
		require.NoError(t, err, "failed to connect to relay")

		metaSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"a"}}}})
		require.NoError(t, err, "failed to subscribe to group metadata")

		membersSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39002}, Tags: nostr.TagMap{"d": []string{"a"}}}})
		require.NoError(t, err, "failed to subscribe to group members")

		// create group
		createGroup := nostr.Event{
			CreatedAt: 1,
			Kind:      nostr.KindSimpleGroupCreateGroup,
			Tags:      nostr.Tags{{"h", "a"}},
		}
		createGroup.Sign(user1)
		require.NoError(t, r.Publish(ctx, createGroup), "failed to publish kind 9007")

		// see if we get notified about that
		select {
		case evt := <-metaSub.Events:
			require.Equal(t, "a", evt.Tags.GetD())
			require.Nil(t, evt.Tags.GetFirst([]string{"private"}))
			require.NotNil(t, evt.Tags.GetFirst([]string{"public"}))
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		select {
		case evt := <-membersSub.Events:
			require.Equal(t, "a", evt.Tags.GetD())
			require.NotNil(t,
				evt.Tags.GetFirst([]string{"p", user1pk}),
			)
			require.Len(t, evt.Tags, 2)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		// invite another member
		inviteMember := nostr.Event{
			CreatedAt: 2,
			Kind:      9000,
			Tags:      nostr.Tags{{"h", "a"}, {"p", user2pk}},
		}
		inviteMember.Sign(user1)
		require.NoError(t, r.Publish(ctx, inviteMember), "failed to publish kind 9000")

		// see if we get notified about that
		select {
		case evt := <-membersSub.Events:
			require.Equal(t, "a", evt.Tags.GetD())
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user1pk}))
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user2pk}))
			require.Len(t, evt.Tags, 3)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		// update metadata
		updateMetadata := nostr.Event{
			CreatedAt: 3,
			Kind:      9002,
			Tags:      nostr.Tags{{"h", "a"}, {"name", "alface"}},
		}
		updateMetadata.Sign(user1)
		require.NoError(t, r.Publish(ctx, updateMetadata), "failed to publish kind 9002")

		// see if we get notified about that
		select {
		case evt := <-metaSub.Events:
			require.Equal(t, "a", evt.Tags.GetD())
			require.Equal(t, &nostr.Tag{"name", "alface"}, evt.Tags.GetFirst([]string{"name"}))
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		msgSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{9, 10}, Tags: nostr.TagMap{"h": []string{"a"}}}})
		require.NoError(t, err, "failed to subscribe to group messages")

		// publish some messages
		previous := make([]string, 1, 6)
		previous[0] = "previous"
		for i := 4; i < 10; i++ {
			message := nostr.Event{
				CreatedAt: nostr.Timestamp(i),
				Content:   fmt.Sprintf("hello %d", i),
				Kind:      9,
				Tags:      nostr.Tags{{"h", "a"}, previous},
			}
			signer := user1
			if i%2 == 1 {
				signer = user2
			}
			message.Sign(signer)
			require.NoError(t, r.Publish(ctx, message), "failed to publish kind 9")

			if i%3 == 0 {
				previous = append(previous, message.ID[0:i*2])
			}
		}

		// check if we have received messages correctly from the subscription
		for i := 4; i < 10; i++ {
			publisher := user1pk
			if i%2 == 1 {
				publisher = user2pk
			}
			message := <-msgSub.Events
			require.Equal(t, fmt.Sprintf("hello %d", i), message.Content)
			require.Equal(t, publisher, message.PubKey)
		}

		// events that should be rejected
		failedNoHTag := nostr.Event{
			CreatedAt: 11,
			Content:   "failed",
			Kind:      9,
		}
		failedNoHTag.Sign(user1)
		require.Error(t, r.Publish(ctx, failedNoHTag), "should fail to publish kind 9 with no h tag")

		failedWrongHTag := nostr.Event{
			CreatedAt: 11,
			Content:   "failed",
			Kind:      9,
			Tags:      nostr.Tags{{"h", "b"}},
		}
		failedWrongHTag.Sign(user1)
		require.Error(t, r.Publish(ctx, failedWrongHTag), "should fail to publish kind 9 with wrong h tag")

		failedFromNonMember := nostr.Event{
			CreatedAt: 11,
			Content:   "failed",
			Kind:      9,
			Tags:      nostr.Tags{{"h", "a"}},
		}
		failedWrongHTag.Sign(user3)
		require.Error(t, r.Publish(ctx, failedFromNonMember), "should fail to publish kind 9 from non-member")

		failedWrongPreviousTag := nostr.Event{
			CreatedAt: 9,
			Content:   "failed",
			Kind:      9,
			Tags:      nostr.Tags{{"h", "a"}, {"previous", "aaaaa"}},
		}
		failedWrongPreviousTag.Sign(user1)
		require.Error(t, r.Publish(ctx, failedWrongPreviousTag), "should fail to publish kind 9 with wrong previous tag")

		previous = append(previous, "zzzzz")
		failedSomeCorrectSomeWrongPreviousTag := nostr.Event{
			CreatedAt: 9,
			Content:   "failed",
			Kind:      9,
			Tags:      nostr.Tags{{"h", "a"}, previous},
		}
		failedSomeCorrectSomeWrongPreviousTag.Sign(user1)
		require.Error(t, r.Publish(ctx, failedSomeCorrectSomeWrongPreviousTag), "should fail to publish kind 9 with some correct some wrong previous tag")

		// get stored messages
		ext, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{9, 10, 11, 12}, Tags: nostr.TagMap{"h": []string{"a"}}}})
		require.NoError(t, err, "failed to subscribe to messages again")
		count := 0
		for {
			select {
			case message := <-ext.Events:
				require.Equal(t, 9, message.Kind)
				require.Equal(t, fmt.Sprintf("hello %d", message.CreatedAt), message.Content)
				count++
			case <-ext.EndOfStoredEvents:
				require.Equal(t, 6, count, "must have 6 messages")
				goto end1_1
			case <-time.After(time.Second):
				t.Fatal("select took too long")
				return
			}
		}
	end1_1:
	}

	// adding now a private group
	{
		r, err := nostr.RelayConnect(ctx, "ws://localhost:29292")
		require.NoError(t, err, "failed to connect to relay")

		createGroupFail := nostr.Event{
			CreatedAt: 1,
			Kind:      9007,
			Tags:      nostr.Tags{{"h", "a"}},
		}
		createGroupFail.Sign(user3)
		require.Error(t, r.Publish(ctx, createGroupFail), "should fail to publish kind 9007 for existing group")

		metaSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39000}, Tags: nostr.TagMap{"d": []string{"b"}}}})
		require.NoError(t, err, "failed to subscribe to group metadata")

		membersSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39002}, Tags: nostr.TagMap{"d": []string{"b"}}}})
		require.NoError(t, err, "failed to subscribe to group members")

		adminsSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39001}, Tags: nostr.TagMap{"d": []string{"b"}}}})
		require.NoError(t, err, "failed to subscribe to group members")

		createGroup := nostr.Event{
			CreatedAt: 1,
			Kind:      9007,
			Tags:      nostr.Tags{{"h", "b"}},
		}
		createGroup.Sign(user3)
		require.NoError(t, r.Publish(ctx, createGroup), "failed to publish kind 9007")

		select {
		case evt := <-metaSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		select {
		case evt := <-membersSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user3pk}))
			require.Len(t, evt.Tags, 2)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		select {
		case evt := <-adminsSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user3pk, "ceo"}))
			require.Len(t, evt.Tags, 2)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		inviteMember := nostr.Event{
			CreatedAt: 2,
			Kind:      9000,
			Tags:      nostr.Tags{{"h", "b"}, {"p", user2pk, "secretary", "assistant"}},
		}
		inviteMember.Sign(user3)
		require.NoError(t, r.Publish(ctx, inviteMember), "failed to publish kind 9000")

		select {
		case evt := <-membersSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user3pk}))
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user2pk}))
			require.Len(t, evt.Tags, 3)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		select {
		case evt := <-adminsSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user3pk, "ceo"}))
			require.NotNil(t, evt.Tags.GetFirst([]string{"p", user2pk, "secretary"}))
			require.Len(t, evt.Tags, 3)
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		setGroupPrivate := nostr.Event{
			CreatedAt: 3,
			Kind:      9002,
			Tags: nostr.Tags{
				{"h", "b"},
				{"private"},
			},
		}
		setGroupPrivate.Sign(user2)
		require.Error(t, r.Publish(ctx, setGroupPrivate), "should fail to accept moderation from secretary")

		setGroupPrivate.Sign(user3)
		require.NoError(t, r.Publish(ctx, setGroupPrivate), "failed to publish kind 9006")

		select {
		case evt := <-metaSub.Events:
			require.Equal(t, "b", evt.Tags.GetD())
			require.Nil(t, evt.Tags.GetFirst([]string{"public"}))
			require.NotNil(t, evt.Tags.GetFirst([]string{"private"}))
		case <-time.After(time.Second):
			t.Fatal("select took too long")
			return
		}

		for i := 4; i < 10; i++ {
			message := nostr.Event{
				CreatedAt: nostr.Timestamp(i),
				Content:   fmt.Sprintf("hello %d", i),
				Kind:      9,
				Tags:      nostr.Tags{{"h", "b"}},
			}
			signer := user3
			if i%2 == 1 {
				signer = user2
			}
			message.Sign(signer)
			require.NoError(t, r.Publish(ctx, message), "failed to publish kind 9")
		}

		failedSub, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{9}, Tags: nostr.TagMap{"h": []string{"b"}}}})
		require.NoError(t, err, "failed to subscribe to private messages")

		for {
			select {
			case <-failedSub.Events:
				t.Fatal("should not have received events")
				return
			case <-failedSub.EndOfStoredEvents:
				t.Fatal("should not have received EOSE")
				return
			case closed := <-failedSub.ClosedReason:
				require.Contains(t, closed, "auth-required:")
				goto end2_1
			case <-time.After(time.Second):
				t.Fatal("select took too long")
				return
			}
		}
	end2_1:
		r2, err := nostr.RelayConnect(ctx, "ws://localhost:29292")
		require.NoError(t, err, "failed to connect to relay")

		time.Sleep(time.Millisecond * 20) // wait until auth is received

		err = r2.Auth(ctx, func(authEvent *nostr.Event) error {
			authEvent.Sign(user2)
			return nil
		})
		require.NoError(t, err, "auth should have worked")

		goodSub, err := r2.Subscribe(ctx, nostr.Filters{{Kinds: []int{9}, Tags: nostr.TagMap{"h": []string{"b"}}}})
		require.NoError(t, err, "failed to subscribe to private messages")

		count := 0
		for {
			select {
			case message := <-goodSub.Events:
				require.Equal(t, 9, message.Kind)
				require.Equal(t, fmt.Sprintf("hello %d", message.CreatedAt), message.Content)
				count++
			case <-goodSub.EndOfStoredEvents:
				require.Equal(t, 6, count, "must have 6 messages")
				goto end2_2
			case <-failedSub.ClosedReason:
				t.Fatal("should not have received CLOSED")
			case <-time.After(time.Second):
				t.Fatal("select took too long")
				return
			}
		}
	end2_2:

		anotherMessage := nostr.Event{
			CreatedAt: 11,
			Content:   "last",
			Kind:      9,
			Tags:      nostr.Tags{{"h", "b"}},
		}
		anotherMessage.Sign(user3)
		require.NoError(t, r.Publish(ctx, anotherMessage), "failed to publish last kind 9")

		select {
		case message := <-goodSub.Events:
			// good sub should receive it
			require.Equal(t, "last", message.Content)
		case <-time.After(time.Millisecond):
			t.Fatal("select took too long")
			return
		}

		select {
		case evt := <-failedSub.Events:
			if evt != nil {
				t.Fatalf("unauthed sub should not receive %s", evt)
			}
		case <-time.After(time.Millisecond * 200):
			t.Fatal("failedSub should have emitted a nil immediately")
		}
	}

	{
		// query members list filtering by "#p"
		for i, s := range []struct {
			key                         string
			groupcount                  int
			groupcountwhenauthedasuser2 int
		}{
			{user1pk, 1, 1}, {user2pk, 1, 2}, {user3pk, 0, 1},
		} {
			r, err := nostr.RelayConnect(ctx, "ws://localhost:29292")
			require.NoError(t, err, "failed to connect to relay")

			ms, err := r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39002}, Tags: nostr.TagMap{"p": []string{s.key}}}})
			require.NoError(t, err, "failed to subscribe to group members")

			count := 0
			for {
				select {
				case message := <-ms.Events:
					require.Equal(t, 39002, message.Kind)
					count++
				case <-ms.EndOfStoredEvents:
					require.Equal(t, s.groupcount, count,
						"when unauthed for key%d expected %d groups but got %d",
						i+1, s.groupcount, count)
					goto end3_1
				case <-time.After(time.Second):
					t.Fatalf("select took too long for key%d", i+1)
					return
				}
			}
		end3_1:

			// perform auth and try again
			err = r.Auth(ctx, func(authEvent *nostr.Event) error {
				authEvent.Sign(user2)
				return nil
			})
			ms, err = r.Subscribe(ctx, nostr.Filters{{Kinds: []int{39002}, Tags: nostr.TagMap{"p": []string{s.key}}}})
			require.NoError(t, err, "failed to subscribe to group members")

			count = 0
			for {
				select {
				case message := <-ms.Events:
					require.Equal(t, 39002, message.Kind)
					count++
				case <-ms.EndOfStoredEvents:
					require.Equal(t, s.groupcountwhenauthedasuser2, count,
						"when authed for key%d expected %d groups but got %d",
						i+1, s.groupcountwhenauthedasuser2, count)
					goto end3_2
				case <-time.After(time.Second):
					return
				}
			}
		end3_2:
		}
	}
}
