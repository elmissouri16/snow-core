package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const chatGPTAccountMetadataKey = "chatgpt_account_binding"

// ErrChatGPTAccountContinuity means selection requires a fresh conversation.
// Surfaces may offer that transition, but must not transfer private continuity.
var ErrChatGPTAccountContinuity = errors.New("app: ChatGPT private continuation belongs to another account; in the TUI, select the desired account with /account NAME to carry over saved working context safely (the current alias can also be reselected), or start a fresh session with snow --provider chatgpt --account NAME")

// ChatGPTAccountStatus is a local, secret-free account inventory entry.
// QuotaKind reports an observed failure, never a balance or subscription claim.
type ChatGPTAccountStatus struct {
	Name      string
	Selected  bool
	Preferred bool
	Status    auth.Status
	QuotaKind string
	ResetsAt  time.Time
}

func (a *App) SelectedChatGPTAccount() string {
	if runtime, ok := a.Providers["chatgpt"].(*provider.Authenticated); ok {
		return runtime.Account()
	}
	return "default"
}

// ChatGPTAccounts performs local inspection only: no refresh, login or catalog
// discovery happens merely because an account picker is opened.
func (a *App) ChatGPTAccounts(ctx context.Context) ([]ChatGPTAccountStatus, error) {
	if a == nil || a.AuthService == nil {
		return nil, errors.New("app: auth service is unavailable")
	}
	names, err := a.AuthService.Accounts("chatgpt")
	if err != nil {
		return nil, err
	}
	selected := a.SelectedChatGPTAccount()
	if !slices.Contains(names, selected) {
		names = append(names, selected)
	}
	slices.Sort(names)
	out := make([]ChatGPTAccountStatus, 0, len(names))
	for _, name := range names {
		status, statusErr := a.AuthService.StatusAccount(ctx, "chatgpt", name)
		// Invalid credentials still belong in the picker, with their local status.
		if statusErr != nil && status.State != auth.StateInvalid {
			return nil, statusErr
		}
		entry := ChatGPTAccountStatus{Name: name, Selected: name == selected,
			Preferred: name == a.Cfg.ChatGPTAccounts.Preferred || (name == "default" && a.Cfg.ChatGPTAccounts.Preferred == ""), Status: status}
		if runtime, ok := a.Providers["chatgpt"].(*provider.Authenticated); ok {
			key, _ := auth.AccountKey("chatgpt", name)
			if credential, exists := a.Auth.Get(key); exists {
				entry.QuotaKind, entry.ResetsAt = runtime.AccountQuota(name, auth.AccountIdentity(credential))
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// SelectChatGPTAccount changes this runtime only, at an idle turn boundary.
// The next catalog request is explicit; selecting never starts network activity.
func (a *App) SelectChatGPTAccount(ctx context.Context, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.Agent == nil || a.AuthService == nil {
		return errors.New("app: account selection is unavailable")
	}
	if err := auth.ValidateAccountName(name); err != nil {
		return err
	}
	if name == "" {
		name = "default"
	}
	if a.Agent.IsRunning() {
		return errors.New("app: wait for the current turn before switching accounts")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	_, _, running, err := a.Agent.SessionIdentityAdmitted()
	if err != nil {
		return err
	}
	if running {
		return errors.New("app: wait for the current turn before switching accounts")
	}
	runtime, credential, err := a.chatGPTAccountSelectionAdmitted(ctx, name)
	if err != nil {
		return err
	}
	messages, err := a.Session.Messages()
	if err != nil {
		return err
	}
	if err := checkChatGPTBinding(a.Session, credential, messages, false); err != nil {
		return err
	}
	if runtime.Account() == name {
		return nil
	}
	if err := runtime.SetAccount(name); err != nil {
		return err
	}
	a.clearProviderCatalogLocked("chatgpt")
	return nil
}

// chatGPTAccountSelectionAdmitted requires stateMu and agent admission. It
// validates local selection only; no network request or history mutation occurs.
func (a *App) chatGPTAccountSelectionAdmitted(ctx context.Context, name string) (*provider.Authenticated, auth.Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, auth.Credential{}, err
	}
	_, _, running, err := a.Agent.SessionIdentityAdmitted()
	if err != nil {
		return nil, auth.Credential{}, err
	}
	if running {
		return nil, auth.Credential{}, errors.New("app: wait for the current turn before switching accounts")
	}
	if err := a.Agent.CredentialSelectionReady(); err != nil {
		return nil, auth.Credential{}, err
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return nil, auth.Credential{}, errors.New("app: wait for active subagents before switching accounts")
	}
	if a.ProviderID != "chatgpt" || a.AuthService == nil {
		return nil, auth.Credential{}, errors.New("app: account selection requires the ChatGPT provider")
	}
	runtime, ok := a.Providers["chatgpt"].(*provider.Authenticated)
	if !ok {
		return nil, auth.Credential{}, errors.New("app: ChatGPT account routing is unavailable")
	}
	key, err := auth.AccountKey("chatgpt", name)
	if err != nil {
		return nil, auth.Credential{}, err
	}
	status, err := a.AuthService.StatusAccount(ctx, "chatgpt", name)
	if err != nil {
		return nil, auth.Credential{}, err
	}
	if !status.Configured() {
		return nil, auth.Credential{}, fmt.Errorf("app: account %q requires login; use /login chatgpt %s", name, name)
	}
	credential, exists := a.Auth.Get(key)
	if !exists {
		return nil, auth.Credential{}, fmt.Errorf("app: account %q was removed; log in again", name)
	}
	credential.Provider, credential.Account = "chatgpt", name
	return runtime, credential, nil
}

// SelectChatGPTAccountSession carries a bounded plain-text working checkpoint
// into a fresh session and commits the account through the normal lifecycle.
// Exact old history stays saved; failed admission leaves both selections intact.
func (a *App) SelectChatGPTAccountSession(ctx context.Context, name string, st session.Store) error {
	if a == nil || a.Agent == nil {
		return errors.New("app: account selection is unavailable")
	}
	if err := auth.ValidateAccountName(name); err != nil {
		return err
	}
	if name == "" {
		name = "default"
	}
	return a.setSession(ctx, st, name)
}

func chatGPTRequestGuard(current func() session.Store) provider.RequestGuard {
	return func(ctx context.Context, credential auth.Credential, request protocol.ChatRequest) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		store := current()
		if store == nil {
			return errors.New("app: ChatGPT session is unavailable")
		}
		// Matching durable branch metadata avoids rescanning the full saved
		// history every tool round. Inherited children are seeded separately.
		if metadata, ok := store.(session.MetadataStore); ok {
			binding, found, err := metadata.Metadata(chatGPTAccountMetadataKey)
			if err != nil {
				return fmt.Errorf("app: read ChatGPT account binding: %w", err)
			}
			if identity := auth.AccountIdentity(credential); identity != "" && found && binding == identity {
				return nil
			}
		}
		messages, err := store.Messages()
		if err != nil {
			return err
		}
		// Check both saved history and the actual provider-facing request. This
		// also protects compaction requests and custom request augmentation.
		messages = append(messages, request.Messages...)
		return checkChatGPTBinding(store, credential, messages, true)
	}
}

// seedChatGPTChildBinding never treats inherited opaque items as legacy
// default-account history. Direct forks retain a known root binding; restored
// legacy children or recursive forks without verified provenance are blocked.
// Callers can use fork_turns=none to create an independent child.
func seedChatGPTChildBinding(child, parent session.Store) error {
	messages, err := child.Messages()
	if err != nil || !hasChatGPTContinuity(messages) {
		return err
	}
	metadata, ok := child.(session.MetadataStore)
	if !ok {
		return errors.New("app: inherited ChatGPT continuity requires session metadata")
	}
	if _, found, err := metadata.Metadata(chatGPTAccountMetadataKey); err != nil || found {
		return err
	}
	binding := "unverified-inherited-continuity"
	if parentMetadata, ok := parent.(session.MetadataStore); ok {
		value, found, err := parentMetadata.Metadata(chatGPTAccountMetadataKey)
		if err != nil {
			return err
		}
		if found && value != "" {
			binding = value
		}
	}
	return metadata.SetMetadata(chatGPTAccountMetadataKey, binding)
}

func hasChatGPTContinuity(messages []protocol.Message) bool {
	for _, message := range messages {
		if message.Provider != "chatgpt" || message.StopReason == protocol.StopError || message.StopReason == protocol.StopAborted {
			continue
		}
		for _, block := range message.Content {
			if block.Type == protocol.BlockProviderData {
				return true
			}
		}
	}
	return false
}

func checkChatGPTBinding(store session.Store, credential auth.Credential, messages []protocol.Message, persist bool) error {
	identity := auth.AccountIdentity(credential)
	hidden := hasChatGPTContinuity(messages)
	metadata, supportsMetadata := store.(session.MetadataStore)
	binding, found := "", false
	if supportsMetadata {
		var err error
		binding, found, err = metadata.Metadata(chatGPTAccountMetadataKey)
		if err != nil {
			return fmt.Errorf("app: read ChatGPT account binding: %w", err)
		}
	}
	if hidden && (identity == "" || (found && binding != identity) || (!found && credential.Account != "" && credential.Account != "default")) {
		return ErrChatGPTAccountContinuity
	}
	if hidden && !supportsMetadata {
		return errors.New("app: this session cannot persist ChatGPT account binding; use a built-in session store or start a fresh session")
	}
	if !persist || !supportsMetadata || identity == "" || (found && binding == identity) {
		return nil
	}
	if err := metadata.SetMetadata(chatGPTAccountMetadataKey, identity); err != nil {
		return fmt.Errorf("app: persist ChatGPT account binding: %w", err)
	}
	return nil
}
