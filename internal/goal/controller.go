package goal

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func New(st session.Store, home string, emit func(protocol.AgentEvent)) (*Controller, error) {
	if _, ok := st.(session.ThreadGoalStore); !ok {
		return nil, errors.New("goal: session does not support persistent goals")
	}
	if _, ok := st.(session.ThreadGoalAtomicStore); !ok {
		return nil, errors.New("goal: session does not support atomic goal transitions")
	}
	return &Controller{store: st, home: home, emit: emit, objectiveUpdated: make(map[string]bool), remainders: make(map[string]time.Duration)}, nil
}

func (c *Controller) SetEmitter(emit func(protocol.AgentEvent)) {
	c.mu.Lock()
	c.emit = emit
	c.mu.Unlock()
}

func (c *Controller) ValidateStore(st session.Store) error {
	if st == nil {
		return errors.New("goal: nil session")
	}
	if _, ok := st.(session.ThreadGoalStore); !ok {
		return errors.New("goal: session does not support persistent goals")
	}
	if _, ok := st.(session.ThreadGoalAtomicStore); !ok {
		return errors.New("goal: session does not support atomic goal transitions")
	}
	return nil
}

func (c *Controller) SetStore(st session.Store) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := st.(session.ThreadGoalStore); !ok {
		return errors.New("goal: session does not support persistent goals")
	}
	if _, ok := st.(session.ThreadGoalAtomicStore); !ok {
		return errors.New("goal: session does not support atomic goal transitions")
	}
	c.store = st
	c.objectiveUpdated = make(map[string]bool)
	c.auditGoalID = ""
	c.auditTurns = 0
	c.remainders = make(map[string]time.Duration)
	return nil
}

func (c *Controller) Store() session.Store { c.mu.Lock(); defer c.mu.Unlock(); return c.store }

func (c *Controller) goalStore() session.ThreadGoalStore { return c.store.(session.ThreadGoalStore) }

func (c *Controller) atomicStore() session.ThreadGoalAtomicStore {
	return c.store.(session.ThreadGoalAtomicStore)
}

func (c *Controller) Get() (*protocol.ThreadGoal, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.goalStore().Goal()
}

func id() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "goal-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("goal-%d", time.Now().UnixNano())
}

func returnUnlock(c *Controller, g *protocol.ThreadGoal, err error) (*protocol.ThreadGoal, error) {
	c.mu.Unlock()
	return g, err
}

func (c *Controller) Create(objective string, budget *int64, replace bool) (*protocol.ThreadGoal, error) {
	c.mu.Lock()
	if c.store.Path() == "" {
		c.mu.Unlock()
		return nil, errors.New("goal: goals require a persisted session; start or resume a saved session")
	}
	objective = strings.TrimSpace(objective)
	if objective == "" {
		c.mu.Unlock()
		return nil, errors.New("goal: objective is required")
	}
	if len([]byte(objective)) > 4*protocol.MaxThreadGoalObjectiveChars {
		c.mu.Unlock()
		return nil, errors.New("goal: objective exceeds managed file limit")
	}
	goalID := id()
	cleanup := func() {}
	if len([]byte(objective)) > materializeThreshold {
		ref, remove, err := c.materialize(objective, goalID)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		objective, cleanup = ref, remove
	}
	if budget != nil && *budget <= 0 {
		cleanup()
		c.mu.Unlock()
		return nil, errors.New("goal: token budget must be positive")
	}
	old, err := c.goalStore().Goal()
	if err != nil {
		cleanup()
		c.mu.Unlock()
		return nil, err
	}
	now := time.Now().UnixMilli()
	g := protocol.ThreadGoal{GoalID: goalID, Objective: objective, Status: protocol.GoalActive, TokenBudget: budget, CreatedAt: now, UpdatedAt: now}
	if replace {
		expected := ""
		if old != nil {
			expected = old.GoalID
		}
		err = c.atomicStore().ReplaceGoal(expected, g)
	} else {
		err = c.goalStore().CreateGoal(g, false)
	}
	if err != nil {
		cleanup()
		c.mu.Unlock()
		return nil, err
	}
	out, err := c.goalStore().Goal()
	c.auditGoalID = ""
	c.auditTurns = 0
	if err == nil && old != nil {
		delete(c.remainders, old.GoalID)
		delete(c.objectiveUpdated, old.GoalID)
	}
	emit := c.emit
	c.mu.Unlock()
	if err == nil && old != nil {
		c.removeManaged(old.GoalID, old.Objective)
	}
	if err == nil {
		emitUpdate(emit, out, false)
	}
	return out, err
}

func (c *Controller) Edit(expected, objective string) (*protocol.ThreadGoal, error) {
	c.mu.Lock()
	objective = strings.TrimSpace(objective)
	if objective == "" {
		c.mu.Unlock()
		return nil, errors.New("goal: objective is required")
	}
	if len([]byte(objective)) > 4*protocol.MaxThreadGoalObjectiveChars {
		c.mu.Unlock()
		return nil, errors.New("goal: objective exceeds managed file limit")
	}
	old, err := c.goalStore().Goal()
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if old == nil {
		return returnUnlock(c, nil, session.ErrNotFound)
	}
	if old.GoalID != expected {
		return returnUnlock(c, nil, errors.New("goal: stale goal id"))
	}
	nextGoalID := id()
	cleanup := func() {}
	if len([]byte(objective)) > materializeThreshold {
		ref, remove, e := c.materialize(objective, nextGoalID)
		if e != nil {
			c.mu.Unlock()
			return nil, e
		}
		objective, cleanup = ref, remove
	}
	out, err := c.atomicStore().ReviseGoal(old.GoalID, nextGoalID, objective)
	if err != nil {
		cleanup()
		c.mu.Unlock()
		return nil, err
	}
	if out != nil {
		c.objectiveUpdated[out.GoalID] = true
	}
	c.auditGoalID = ""
	c.auditTurns = 0
	delete(c.remainders, old.GoalID)
	delete(c.objectiveUpdated, old.GoalID)
	emit := c.emit
	c.mu.Unlock()
	if err == nil {
		c.removeManaged(old.GoalID, old.Objective)
		emitUpdate(emit, out, false)
	}
	return out, err
}

func (c *Controller) RecordGoalTurn(goalID string) {
	c.mu.Lock()
	if c.auditGoalID != goalID {
		c.auditGoalID = goalID
		c.auditTurns = 0
	}
	c.auditTurns++
	c.mu.Unlock()
}

func (c *Controller) SetStatus(expected string, status protocol.ThreadGoalStatus, model bool) (*protocol.ThreadGoal, error) {
	return c.SetStatusWithReason(expected, status, model, "")
}

func (c *Controller) SetStatusWithReason(expected string, status protocol.ThreadGoalStatus, model bool, blockedReason string) (*protocol.ThreadGoal, error) {
	if model && status != protocol.GoalComplete && status != protocol.GoalBlocked {
		return nil, errors.New("goal: model may only set complete or blocked")
	}
	blockedReason = strings.TrimSpace(blockedReason)
	if status == protocol.GoalBlocked {
		if blockedReason == "" {
			if model {
				return nil, errors.New("goal: blocked reason is required")
			}
			blockedReason = "Snow stopped the goal after an unrecoverable error."
		}
		runes := []rune(blockedReason)
		if len(runes) > protocol.MaxThreadGoalBlockedReasonChars {
			if model {
				return nil, fmt.Errorf("goal: blocked reason exceeds %d characters", protocol.MaxThreadGoalBlockedReasonChars)
			}
			blockedReason = string(runes[:protocol.MaxThreadGoalBlockedReasonChars])
		}
	} else if blockedReason != "" {
		return nil, errors.New("goal: blocked reason is valid only for blocked status")
	}
	c.mu.Lock()
	old, err := c.goalStore().Goal()
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if old == nil {
		return returnUnlock(c, nil, session.ErrNotFound)
	}
	if old.GoalID != expected {
		return returnUnlock(c, nil, errors.New("goal: stale goal id"))
	}
	if model && old.Status != protocol.GoalActive {
		return returnUnlock(c, nil, errors.New("goal: model terminal update requires active goal"))
	}
	if model && status == protocol.GoalBlocked && (c.auditGoalID != expected || c.auditTurns < 3) {
		return returnUnlock(c, nil, fmt.Errorf("goal: blocked requires at least 3 consecutive goal turns (have %d)", c.auditTurns))
	}
	if old.Status.Terminal() {
		return returnUnlock(c, nil, errors.New("goal: terminal goal status cannot be changed"))
	}
	if status == protocol.GoalBudgetLimited {
		return returnUnlock(c, nil, errors.New("goal: budget_limited is set only by atomic usage accounting"))
	}
	if status == protocol.GoalComplete && old.Status != protocol.GoalActive {
		return returnUnlock(c, nil, errors.New("goal: only an active goal can be completed"))
	}
	if (status == protocol.GoalBlocked || status == protocol.GoalUsageLimited) && old.Status != protocol.GoalActive {
		return returnUnlock(c, nil, errors.New("goal: terminal error transition requires active goal"))
	}
	if status == protocol.GoalPaused && old.Status != protocol.GoalActive {
		return returnUnlock(c, nil, errors.New("goal: only an active goal can be paused"))
	}
	if status == protocol.GoalActive && old.Status != protocol.GoalPaused && old.Status != protocol.GoalBlocked && old.Status != protocol.GoalUsageLimited {
		return returnUnlock(c, nil, errors.New("goal: only paused, blocked, or usage-limited goals can be resumed"))
	}
	g, err := c.atomicStore().TransitionGoal(expected, old.Status, status, blockedReason, status == protocol.GoalActive)
	if err == nil && status == protocol.GoalActive {
		c.auditGoalID = ""
		c.auditTurns = 0
	}
	emit := c.emit
	c.mu.Unlock()
	if err == nil {
		emitUpdate(emit, g, false)
	}
	return g, err
}

func (c *Controller) Clear(expected string) error {
	c.mu.Lock()
	old, _ := c.goalStore().Goal()
	if err := c.goalStore().ClearGoal(expected); err != nil {
		c.mu.Unlock()
		return err
	}
	emit := c.emit
	c.auditGoalID = ""
	c.auditTurns = 0
	if old != nil {
		delete(c.remainders, old.GoalID)
		delete(c.objectiveUpdated, old.GoalID)
	}
	c.mu.Unlock()
	if old != nil {
		c.removeManaged(old.GoalID, old.Objective)
	}
	emitUpdate(emit, nil, old != nil)
	return nil
}

func (c *Controller) AccountDuration(expected string, tokens int64, elapsed time.Duration, estimatedCost *protocol.Cost) (*protocol.ThreadGoal, bool, error) {
	if elapsed < 0 {
		return nil, false, errors.New("goal: elapsed duration cannot be negative")
	}
	c.mu.Lock()
	total := c.remainders[expected] + elapsed
	seconds := int64(total / time.Second)
	g, cross, err := c.goalStore().AccountGoal(expected, tokens, seconds, estimatedCost)
	if err == nil && g != nil && g.GoalID == expected {
		c.remainders[expected] = total % time.Second
	}
	emit := c.emit
	c.mu.Unlock()
	if err == nil && g != nil && g.GoalID == expected {
		emitUpdate(emit, g, false)
	}
	return g, cross, err
}

func (c *Controller) Account(expected string, tokens, seconds int64) (*protocol.ThreadGoal, bool, error) {
	c.mu.Lock()
	g, cross, err := c.goalStore().AccountGoal(expected, tokens, seconds, nil)
	emit := c.emit
	c.mu.Unlock()
	if err == nil && g != nil && g.GoalID == expected {
		emitUpdate(emit, g, false)
	}
	return g, cross, err
}

func (c *Controller) Defer(v bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.goalStore().SetGoalContinuationDeferred(v)
}

func (c *Controller) Fragment(g protocol.ThreadGoal, turn int, budgetWrap bool) (protocol.InternalContextFragment, error) {
	c.mu.Lock()
	updated := c.objectiveUpdated[g.GoalID]
	home, sessionID := c.home, c.store.ID()
	c.mu.Unlock()
	resolved, err := resolveManaged(home, sessionID, g.GoalID, g.Objective)
	if err != nil {
		return protocol.InternalContextFragment{}, fmt.Errorf("goal: resolve objective: %w", err)
	}
	g.Objective = resolved
	c.mu.Lock()
	// Consume the one-shot objective-updated steering only after the objective
	// was successfully resolved for this exact goal revision.
	if updated {
		delete(c.objectiveUpdated, g.GoalID)
	}
	c.mu.Unlock()
	if budgetWrap {
		return ContinuationFragment(g, turn, true), nil
	}
	if updated {
		return ObjectiveUpdatedFragment(g), nil
	}
	return ContinuationFragment(g, turn, false), nil
}

func (c *Controller) Deferred() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.goalStore().GoalContinuationDeferred()
}

func emitUpdate(emit func(protocol.AgentEvent), g *protocol.ThreadGoal, clear bool) {
	if emit != nil {
		emit(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g.Clone(), Cleared: clear}})
	}
}

// DeleteSessionData removes managed goal files for a permanently deleted
// session. The pinned global root prevents a malformed ID from escaping home.
func DeleteSessionData(home, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" || filepath.Base(sessionID) != sessionID || sessionID == "." || sessionID == ".." {
		return errors.New("goal: invalid session ID")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("goal: open global root: %w", err)
	}
	defer root.Close()
	if err := root.RemoveAll(filepath.Join("goals", sessionID)); err != nil {
		return fmt.Errorf("goal: remove session data: %w", err)
	}
	return nil
}

func managedPath(ref string) (string, bool) {
	if !strings.HasPrefix(ref, managedPrefix) || !strings.HasSuffix(ref, managedSuffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(ref, managedPrefix), managedSuffix), true
}

func (r *managedRoots) close() {
	for _, root := range []*os.Root{r.owner, r.session, r.goals, r.home} {
		if root != nil {
			_ = root.Close()
		}
	}
}

func validManagedOwner(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value
}

func managedReferencePath(home, sessionID, ownerGoalID, ref string) (string, error) {
	path, ok := managedPath(ref)
	if !ok {
		return "", errors.New("goal: objective is not a managed reference")
	}
	if !validManagedOwner(sessionID) || !validManagedOwner(ownerGoalID) {
		return "", errors.New("goal: invalid managed objective owner")
	}
	expected, err := filepath.Abs(filepath.Join(home, "goals", sessionID, ownerGoalID, managedObjectiveName))
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if candidate != expected {
		return "", errors.New("goal: managed objective ownership mismatch")
	}
	return candidate, nil
}

func openVerifiedChild(parent *os.Root, name string, create, exclusive bool) (*os.Root, error) {
	if !validManagedOwner(name) {
		return nil, errors.New("goal: invalid managed directory name")
	}
	if create {
		err := parent.Mkdir(name, 0700)
		if err != nil && (exclusive || !errors.Is(err, os.ErrExist)) {
			return nil, err
		}
	}
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, errors.New("goal: managed objective path contains a symlink directory")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = child.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("goal: managed objective directory changed while opening")
	}
	if err := child.Chmod(".", 0700); err != nil {
		_ = child.Close()
		return nil, err
	}
	return child, nil
}

func openManagedRoots(home, sessionID, ownerGoalID string, create bool) (*managedRoots, error) {
	if !validManagedOwner(sessionID) || !validManagedOwner(ownerGoalID) {
		return nil, errors.New("goal: invalid managed objective owner")
	}
	if create {
		if err := os.MkdirAll(home, 0700); err != nil {
			return nil, err
		}
	}
	r := &managedRoots{}
	var err error
	r.home, err = os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	if r.goals, err = openVerifiedChild(r.home, "goals", create, false); err != nil {
		r.close()
		return nil, err
	}
	if r.session, err = openVerifiedChild(r.goals, sessionID, create, false); err != nil {
		r.close()
		return nil, err
	}
	if r.owner, err = openVerifiedChild(r.session, ownerGoalID, create, create); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func resolveManaged(home, sessionID, ownerGoalID, ref string) (string, error) {
	if _, ok := managedPath(ref); !ok {
		return ref, nil
	}
	if _, err := managedReferencePath(home, sessionID, ownerGoalID, ref); err != nil {
		return "", err
	}
	roots, err := openManagedRoots(home, sessionID, ownerGoalID, false)
	if err != nil {
		return "", err
	}
	defer roots.close()
	before, err := roots.owner.Lstat(managedObjectiveName)
	if err != nil {
		return "", err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0600 {
		return "", errors.New("goal: invalid managed objective file")
	}
	file, err := roots.owner.Open(managedObjectiveName)
	if err != nil {
		return "", err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm() != 0600 {
		return "", errors.New("goal: managed objective file changed while opening")
	}
	limit := int64(4*protocol.MaxThreadGoalObjectiveChars + 1)
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return "", err
	}
	if int64(len(data)) >= limit {
		return "", errors.New("goal: invalid managed objective file")
	}
	return string(data), nil
}

func removeManagedAt(home, sessionID, ownerGoalID, ref string) {
	if _, err := managedReferencePath(home, sessionID, ownerGoalID, ref); err != nil {
		return
	}
	roots, err := openManagedRoots(home, sessionID, ownerGoalID, false)
	if err != nil {
		return
	}
	_ = roots.owner.Remove(managedObjectiveName + ".tmp")
	_ = roots.owner.Remove(managedObjectiveName)
	_ = roots.owner.Close()
	roots.owner = nil
	_ = roots.session.Remove(ownerGoalID)
	roots.close()
}

func (c *Controller) removeManaged(ownerGoalID, ref string) {
	c.mu.Lock()
	home, sessionID := c.home, c.store.ID()
	c.mu.Unlock()
	removeManagedAt(home, sessionID, ownerGoalID, ref)
}

// ManagedTextForFork resolves a generated objective on the active branch.
func (c *Controller) ManagedTextForFork() (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.managedTextLocked()
}

// ManagedTextForBranch resolves managed content from an explicit source branch
// and restores the prior active branch before returning.
func (c *Controller) ManagedTextForBranch(branchID string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	branches, ok := c.store.(session.BranchStore)
	if !ok {
		return "", false, errors.New("goal: session does not support branches")
	}
	listed, err := branches.Branches()
	if err != nil {
		return "", false, err
	}
	active := ""
	for _, branch := range listed {
		if branch.Active {
			active = branch.ID
			break
		}
	}
	if branchID == "" || branchID == active {
		return c.managedTextLocked()
	}
	if err := branches.SelectBranch(branchID); err != nil {
		return "", false, err
	}
	text, managed, resolveErr := c.managedTextLocked()
	restoreErr := branches.SelectBranch(active)
	return text, managed, errors.Join(resolveErr, restoreErr)
}

func (c *Controller) managedTextLocked() (string, bool, error) {
	g, err := c.goalStore().Goal()
	if err != nil || g == nil {
		return "", false, err
	}
	if _, ok := managedPath(g.Objective); !ok {
		return "", false, nil
	}
	text, err := resolveManaged(c.home, c.store.ID(), g.GoalID, g.Objective)
	return text, true, err
}

// DiscardManagedCurrent removes only the validated file owned by the current
// goal. It is used while rolling back a fork whose post-copy state failed.
func (c *Controller) DiscardManagedCurrent() {
	c.mu.Lock()
	g, _ := c.goalStore().Goal()
	c.mu.Unlock()
	if g != nil {
		c.removeManaged(g.GoalID, g.Objective)
	}
}

// CopyManagedForFork installs an independent managed file for the goal on the
// newly active fork. Callers must capture text with ManagedTextForFork first.
func (c *Controller) CopyManagedForFork(text string) error {
	c.mu.Lock()
	g, err := c.goalStore().Goal()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if g == nil {
		c.mu.Unlock()
		return session.ErrNotFound
	}
	ref, cleanup, err := c.materialize(text, g.GoalID)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	_, err = c.goalStore().UpdateGoal(g.GoalID, &ref, nil, nil)
	if err != nil {
		cleanup()
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	return nil
}

func (c *Controller) materialize(text, ownerGoalID string) (string, func(), error) {
	if c.home == "" {
		return "", func() {}, errors.New("goal: SNOW_HOME is unavailable for oversized objective")
	}
	sessionID := c.store.ID()
	roots, err := openManagedRoots(c.home, sessionID, ownerGoalID, true)
	if err != nil {
		return "", func() {}, err
	}
	path, err := filepath.Abs(filepath.Join(c.home, "goals", sessionID, ownerGoalID, managedObjectiveName))
	if err != nil {
		roots.close()
		return "", func() {}, err
	}
	ref := managedPrefix + path + managedSuffix
	cleanupNow := func() {
		_ = roots.owner.Remove(managedObjectiveName + ".tmp")
		_ = roots.owner.Remove(managedObjectiveName)
		_ = roots.owner.Close()
		roots.owner = nil
		_ = roots.session.Remove(ownerGoalID)
	}
	file, err := roots.owner.OpenFile(managedObjectiveName+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		var written int
		written, err = file.Write([]byte(text))
		if err == nil && written != len([]byte(text)) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = file.Sync()
	}
	if file != nil {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err == nil {
		err = roots.owner.Rename(managedObjectiveName+".tmp", managedObjectiveName)
	}
	if err != nil {
		cleanupNow()
		roots.close()
		return "", func() {}, err
	}
	roots.close()
	cleanup := func() { removeManagedAt(c.home, sessionID, ownerGoalID, ref) }
	return ref, cleanup, nil
}

func escapedObjective(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func renderGoalPrompt(t *template.Template, data any) string {
	var body strings.Builder
	if err := t.Execute(&body, data); err != nil {
		panic(fmt.Sprintf("goal: render embedded prompt: %v", err))
	}
	return strings.TrimSuffix(body.String(), "\n")
}

func ContinuationFragment(g protocol.ThreadGoal, turn int, budgetWrap bool) protocol.InternalContextFragment {
	remaining := "unlimited"
	if r := g.RemainingBudget(); r != nil {
		remaining = fmt.Sprintf("%d", *r)
	}
	body := renderGoalPrompt(continuationTemplate, continuationPromptData{
		Turn: turn, Remaining: remaining, Objective: escapedObjective(g.Objective), BudgetReached: budgetWrap,
	})
	return protocol.InternalContextFragment{Source: "goal", Text: body}
}

func ObjectiveUpdatedFragment(g protocol.ThreadGoal) protocol.InternalContextFragment {
	body := renderGoalPrompt(objectiveUpdatedTemplate, objectiveUpdatedPromptData{Objective: escapedObjective(g.Objective)})
	return protocol.InternalContextFragment{Source: "goal", Text: body}
}

// Tools returns the direct model-facing goal tools.
func Tools(c *Controller) []tools.Tool {
	return []tools.Tool{&getTool{c}, &createTool{c}, &updateTool{c}}
}

func (*getTool) Schema() protocol.ToolSchema {
	return protocol.ToolSchema{Name: "get_goal", Description: "Get the current persisted thread-goal record, including status, configured token budget, accumulated token/time usage, and estimated costs. When a budget exists, compute remaining tokens as max(0, token_budget minus tokens_used).", Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}
}

func (t *getTool) Run(_ context.Context, _ json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	g, e := t.c.Get()
	if e != nil {
		return tools.ErrorResult(e), nil
	}
	b, _ := json.Marshal(g)
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(b))}, Details: tools.PrivateDetails{}}, nil
}

func (*createTool) Schema() protocol.ToolSchema {
	return protocol.ToolSchema{Name: "create_goal", Description: "Create an active persisted goal in a saved session. A prior terminal goal is replaced; creation fails while an unfinished goal exists. Call only when the user or system requested a goal, and pass token_budget only when an explicit budget was requested.", Parameters: json.RawMessage(`{"type":"object","required":["objective"],"properties":{"objective":{"type":"string","description":"Exact goal requested by the user or system."},"token_budget":{"type":"integer","minimum":1,"description":"Optional explicit token budget; omit when none was requested."}},"additionalProperties":false}`)}
}
