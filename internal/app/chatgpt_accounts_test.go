package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func accountTestCredential(account, workspace, token string) auth.Credential {
	return auth.Credential{Provider: "chatgpt", Account: account, Type: auth.CredentialOAuth, Access: token, AccountID: workspace}
}

func accountTestContinuity() protocol.Message {
	return protocol.NewAssistantMessage("answer", "root", "chatgpt", "model", []protocol.ContentBlock{{Type: protocol.BlockProviderData, Data: []byte(`{"opaque":"private"}`)}}, protocol.StopStop, nil)
}

func TestChatGPTBindingIdentityAndLegacyHistory(t *testing.T) {
	original := accountTestCredential("default", "workspace-a", "old-token")
	rotated := accountTestCredential("renamed", "workspace-a", "rotated-token")
	different := accountTestCredential("default", "workspace-b", "other-token")
	for _, tc := range []struct {
		name                       string
		binding                    string
		credential                 auth.Credential
		hidden, persist, wantError bool
	}{
		{name: "same identity after rotation and alias rename", binding: auth.AccountIdentity(original), credential: rotated, hidden: true, persist: true},
		{name: "alias replaced with different identity", binding: auth.AccountIdentity(original), credential: different, hidden: true, persist: true, wantError: true},
		{name: "legacy default", credential: original, hidden: true, persist: true},
		{name: "legacy implicit default", credential: accountTestCredential("", "workspace-a", "token"), hidden: true, persist: true},
		{name: "legacy named account", credential: rotated, hidden: true, persist: true, wantError: true},
		{name: "missing identity", credential: accountTestCredential("default", "", "token"), hidden: true, persist: true, wantError: true},
		{name: "fresh session can change identity", binding: auth.AccountIdentity(original), credential: different, persist: true},
		{name: "selection does not persist", credential: rotated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := session.NewMemoryStore(session.Options{})
			if tc.binding != "" {
				if err := store.SetMetadata(chatGPTAccountMetadataKey, tc.binding); err != nil {
					t.Fatal(err)
				}
			}
			var messages []protocol.Message
			if tc.hidden {
				messages = []protocol.Message{accountTestContinuity()}
			}
			err := checkChatGPTBinding(store, tc.credential, messages, tc.persist)
			if (err != nil) != tc.wantError {
				t.Fatalf("binding error = %v, wantError=%v", err, tc.wantError)
			}
			binding, found, err := store.Metadata(chatGPTAccountMetadataKey)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.binding
			if tc.persist && !tc.wantError {
				want = auth.AccountIdentity(tc.credential)
			}
			if binding != want || found != (want != "") {
				t.Fatalf("binding=%q found=%v, want %q", binding, found, want)
			}
		})
	}
}

// Embedding Store deliberately omits the optional MetadataStore interface.
type accountTestLegacyStore struct{ session.Store }

type accountTestFailingStore struct {
	*session.MemoryStore
	readErr, writeErr, messagesErr error
	messageReads                   int
}

func (s *accountTestFailingStore) Metadata(key string) (string, bool, error) {
	if s.readErr != nil {
		return "", false, s.readErr
	}
	return s.MemoryStore.Metadata(key)
}
func (s *accountTestFailingStore) SetMetadata(key, value string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	return s.MemoryStore.SetMetadata(key, value)
}
func (s *accountTestFailingStore) Messages() ([]protocol.Message, error) {
	s.messageReads++
	if s.messagesErr != nil {
		return nil, s.messagesErr
	}
	return s.MemoryStore.Messages()
}

func TestChatGPTRequestGuardFailsClosedAndUsesVerifiedMetadata(t *testing.T) {
	credential := accountTestCredential("default", "workspace-a", "token")
	hidden := protocol.ChatRequest{Messages: []protocol.Message{accountTestContinuity()}}
	failure := errors.New("store unavailable")
	for _, tc := range []struct {
		name      string
		store     session.Store
		ctx       context.Context
		request   protocol.ChatRequest
		wantError bool
	}{
		{name: "no session", ctx: t.Context(), wantError: true},
		{name: "metadata unavailable", store: &accountTestFailingStore{MemoryStore: session.NewMemoryStore(session.Options{}), readErr: failure}, ctx: t.Context(), wantError: true},
		{name: "history unavailable", store: &accountTestFailingStore{MemoryStore: session.NewMemoryStore(session.Options{}), messagesErr: failure}, ctx: t.Context(), wantError: true},
		{name: "binding cannot be persisted", store: &accountTestFailingStore{MemoryStore: session.NewMemoryStore(session.Options{}), writeErr: failure}, ctx: t.Context(), request: hidden, wantError: true},
		{name: "legacy store with private data", store: accountTestLegacyStore{session.NewMemoryStore(session.Options{})}, ctx: t.Context(), request: hidden, wantError: true},
		{name: "legacy store without private data", store: accountTestLegacyStore{session.NewMemoryStore(session.Options{})}, ctx: t.Context()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := chatGPTRequestGuard(func() session.Store { return tc.store })
			if err := guard(tc.ctx, credential, tc.request); (err != nil) != tc.wantError {
				t.Fatalf("guard error=%v wantError=%v", err, tc.wantError)
			}
		})
	}
	store := &accountTestFailingStore{MemoryStore: session.NewMemoryStore(session.Options{}), messagesErr: failure}
	if err := store.SetMetadata(chatGPTAccountMetadataKey, auth.AccountIdentity(credential)); err != nil {
		t.Fatal(err)
	}
	guard := chatGPTRequestGuard(func() session.Store { return store })
	if err := guard(t.Context(), credential, hidden); err != nil {
		t.Fatalf("verified metadata shortcut: %v", err)
	}
	if store.messageReads != 0 {
		t.Fatal("matching metadata rescanned history")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := guard(ctx, credential, hidden); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled guard=%v", err)
	}
}

func TestChatGPTChildBindingRequiresVerifiedProvenance(t *testing.T) {
	credential := accountTestCredential("default", "workspace-a", "token")
	for _, tc := range []struct {
		name               string
		verified, noParent bool
	}{
		{name: "verified root", verified: true},
		{name: "legacy root"},
		{name: "restored or recursive child", noParent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var parent session.Store
			if !tc.noParent {
				root := session.NewMemoryStore(session.Options{})
				if tc.verified {
					if err := root.SetMetadata(chatGPTAccountMetadataKey, auth.AccountIdentity(credential)); err != nil {
						t.Fatal(err)
					}
				}
				parent = root
			}
			for range 2 { // Unknown provenance must remain blocked in descendants.
				child := session.NewMemoryStore(session.Options{})
				message := accountTestContinuity()
				if err := child.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
					t.Fatal(err)
				}
				if err := seedChatGPTChildBinding(child, parent); err != nil {
					t.Fatal(err)
				}
				guard := chatGPTRequestGuard(func() session.Store { return child })
				if err := guard(t.Context(), credential, protocol.ChatRequest{}); (err != nil) == tc.verified {
					t.Fatalf("child guard=%v verified=%v", err, tc.verified)
				}
				parent = child
			}
		})
	}
	independent := session.NewMemoryStore(session.Options{})
	if err := seedChatGPTChildBinding(independent, nil); err != nil {
		t.Fatal(err)
	}
	if _, found, err := independent.Metadata(chatGPTAccountMetadataKey); err != nil || found {
		t.Fatalf("independent child binding found=%v err=%v", found, err)
	}
}

func TestChatGPTBindingSurvivesDurableResumeAndRootFork(t *testing.T) {
	cwd := t.TempDir()
	index := session.NewFileIndex(t.TempDir())
	source, err := index.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	credential := accountTestCredential("work", "workspace-a", "old-token")
	guard := chatGPTRequestGuard(func() session.Store { return source })
	if err := guard(t.Context(), credential, protocol.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	message := accountTestContinuity()
	message.ParentID = source.BranchTip()
	if err := source.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	path := source.Path()
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	source, err = session.OpenSQLiteStore(path, cwd, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	credential.Access = "rotated-token"
	if err := guard(t.Context(), credential, protocol.ChatRequest{}); err != nil {
		t.Fatalf("resume rejected rotation: %v", err)
	}
	fork, _, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{Name: "bound fork"})
	if err != nil {
		t.Fatal(err)
	}
	defer fork.Close()
	forkGuard := chatGPTRequestGuard(func() session.Store { return fork })
	if err := forkGuard(t.Context(), credential, protocol.ChatRequest{}); err != nil {
		t.Fatalf("fork lost binding: %v", err)
	}
	credential.AccountID = "workspace-b"
	if err := forkGuard(t.Context(), credential, protocol.ChatRequest{}); err == nil {
		t.Fatal("fork accepted replacement identity")
	}
}

func TestAppChatGPTAccountSelectionAndInventoryStayLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	var calls atomic.Int32
	var withdrawn atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		model := "model"
		if withdrawn.Load() {
			model = "replacement-model"
		}
		fmt.Fprintf(w, `{"models":[{"slug":%q,"visibility":"list"}]}`, model)
	}))
	defer server.Close()
	authPath := filepath.Join(home, "auth.json")
	store, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"default", "work"} {
		key, _ := auth.AccountKey("chatgpt", name)
		if err := store.Put(key, accountTestCredential(name, "workspace-"+name, "token")); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Put("chatgpt/account/broken", auth.Credential{Type: auth.CredentialAPIKey, Key: "wrong-credential-kind"}); err != nil {
		t.Fatal(err)
	}
	expired := accountTestCredential("expired", "workspace-expired", "expired-token")
	expired.Expires, expired.Refresh = 1, "refresh-must-not-be-used-by-inventory"
	if err := store.Put("chatgpt/account/expired", expired); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.Context(), Options{Provider: "chatgpt", NoSession: true, CWD: home, AuthPath: authPath, BaseURL: server.URL, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	before, model := calls.Load(), a.Model
	if err := a.SelectChatGPTAccount(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	inventory, err := a.ChatGPTAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(inventory))
	for i, entry := range inventory {
		names[i] = entry.Name
		switch entry.Name {
		case "broken":
			if entry.Status.State != auth.StateInvalid {
				t.Fatalf("invalid inventory status=%+v", entry.Status)
			}
		case "expired":
			if entry.Status.State != auth.StateExpired {
				t.Fatalf("expired inventory status=%+v", entry.Status)
			}
		default:
			if !entry.Status.Configured() {
				t.Fatalf("configured inventory status=%+v", entry.Status)
			}
		}
		if entry.Selected != (entry.Name == "work") || entry.Preferred != (entry.Name == "default") {
			t.Fatalf("inventory entry=%+v", entry)
		}
	}
	if !slices.Equal(names, []string{"broken", "default", "expired", "work"}) || a.SelectedChatGPTAccount() != "work" {
		t.Fatalf("inventory=%v selected=%q", names, a.SelectedChatGPTAccount())
	}
	if calls.Load() != before || a.Model.ID != model.ID {
		t.Fatal("local selection or inventory made network calls or changed model")
	}
	// Controlled pending work must not be silently retargeted after an abort.
	waiting := &reviewQueueProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(waiting); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = a.Agent.Prompt(t.Context(), "initial account-bound work") }()
	t.Cleanup(func() { a.Agent.Abort(); <-done })
	<-waiting.started
	if err := a.SelectChatGPTAccount(t.Context(), "default"); err == nil {
		t.Fatal("account switch admitted during a turn")
	}
	_, turn, _ := a.Agent.ActiveTurn()
	q, err := a.QueueList(protocol.RPCQueueListParams{SessionID: a.Session.ID(), TurnID: turn})
	if err != nil {
		t.Fatal(err)
	}
	q, err = a.QueueEnqueue(protocol.RPCQueueEnqueueParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "retained account-bound work"})
	if err != nil {
		t.Fatal(err)
	}
	a.Agent.Abort()
	<-done
	q, err = a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
	if err != nil || len(q.ReviewItems) != 1 {
		t.Fatalf("recovered queue=%+v err=%v", q, err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "default"); err == nil {
		t.Fatal("account switch retargeted controlled pending input")
	}
	after, err := a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
	if err != nil || a.SelectedChatGPTAccount() != "work" || len(after.ReviewItems) != 1 || after.ReviewItems[0].ID != q.ReviewItems[0].ID {
		t.Fatal("rejected switch changed selection or discarded queued work")
	}
	if _, err := a.QueueRemove(protocol.RPCQueueRemoveParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.ReviewItems[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetProvider(a.Provider); err != nil {
		t.Fatal(err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "default"); err != nil {
		t.Fatalf("fresh session could not switch: %v", err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	credential, _ := a.Auth.Get("chatgpt/account/work")
	if err := chatGPTRequestGuard(func() session.Store { return a.Session })(t.Context(), credential, protocol.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	message := accountTestContinuity()
	message.ParentID = a.Session.BranchTip()
	if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "default"); err == nil {
		t.Fatal("selected a different identity with private continuity")
	}
	credential.Access = "rotated-token"
	if err := a.Auth.Put("chatgpt/account/work", credential); err != nil {
		t.Fatal(err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "work"); err != nil {
		t.Fatalf("same identity token rotation rejected: %v", err)
	}
	credential.AccountID = "replaced-workspace"
	if err := a.Auth.Put("chatgpt/account/work", credential); err != nil {
		t.Fatal(err)
	}
	if err := a.SelectChatGPTAccount(t.Context(), "work"); err == nil {
		t.Fatal("same-alias replacement bypassed binding validation")
	}
	if a.SelectedChatGPTAccount() != "work" || calls.Load() != before {
		t.Fatal("rejected selection mutated account or made network calls")
	}
	// A withdrawn model is an explicit conflict, never an account-driven substitution.
	withdrawn.Store(true)
	if err := a.RefreshProviderModels(t.Context(), "chatgpt"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("withdrawn model refresh=%v", err)
	}
	if a.Model.ID != model.ID || a.Agent.Model().ID != model.ID {
		t.Fatal("catalog refresh automatically substituted a model")
	}
}

func TestAppChatGPTStartupRejectsUnavailableExplicitModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"available-model","visibility":"list"}]}`)
	}))
	defer server.Close()
	path := filepath.Join(home, "auth.json")
	store, err := auth.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"default", "work"} {
		key, _ := auth.AccountKey("chatgpt", account)
		if err := store.Put(key, accountTestCredential(account, "workspace-"+account, "token")); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"", "default", "work"} {
		t.Run("account="+account, func(t *testing.T) {
			a, err := New(t.Context(), Options{Provider: "chatgpt", Account: account, Model: "unavailable-explicit-model", NoSession: true, CWD: home, AuthPath: path, BaseURL: server.URL, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
			if a != nil {
				defer a.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "unavailable-explicit-model") || !strings.Contains(err.Error(), "unavailable") {
				t.Fatalf("explicit model startup=%v", err)
			}
		})
	}
}
