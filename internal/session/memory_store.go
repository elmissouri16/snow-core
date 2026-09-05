package session

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func cloneEntry(entry Entry) Entry {
	if entry.Message != nil {
		message := entry.Message.Clone()
		entry.Message = &message
	}
	return entry
}

func cloneEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, entry := range entries {
		out[i] = cloneEntry(entry)
	}
	return out
}

func normalizeEntryMessage(entry *Entry) {
	if entry == nil || entry.Message == nil {
		return
	}
	entry.Message.ID = entry.ID
	entry.Message.ParentID = entry.ParentID
}

// SuggestedTitle derives a short, provider-free title from the first prompt.
// It collapses whitespace, removes control characters and Markdown heading/list
// prefixes, and truncates at a word boundary when practical.
func SuggestedTitle(prompt string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(prompt) {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if b.Len() > 0 {
				space = true
			}
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	title := strings.TrimSpace(b.String())
	title = strings.TrimSpace(strings.TrimLeft(title, "#>*-+ "))
	runes := []rune(title)
	if len(runes) <= maxSessionTitleRunes {
		return title
	}
	cut := maxSessionTitleRunes - 1
	for i := cut; i >= maxSessionTitleRunes/2; i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}

func normalizeSessionTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > maxSessionTitleRunes {
		return "", fmt.Errorf("session: title must be 1..%d runes", maxSessionTitleRunes)
	}
	for _, r := range title {
		if unicode.IsControl(r) {
			return "", errors.New("session: title contains control characters")
		}
	}
	return title, nil
}

func validateBranchName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return errors.New("session: branch name must be 1..64 runes")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("session: branch name contains control characters")
		}
	}
	return nil
}

func branchNameExists(branches map[string]protocol.SessionBranch, name, except string) bool {
	for id, branch := range branches {
		if id != except && strings.EqualFold(branch.Name, name) {
			return true
		}
	}
	return false
}

func nextBranchName(branches map[string]protocol.SessionBranch) string {
	for n := 2; ; n++ {
		name := fmt.Sprintf("branch-%d", n)
		if !branchNameExists(branches, name, "") {
			return name
		}
	}
}

func newID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), randomSuffix())
}

func randomSuffix() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	seed := time.Now().UnixNano()
	for i := range b {
		seed = seed*6364136223846793005 + 1442695040888963407
		b[i] = letters[(seed>>33)&0x1f]
	}
	return string(b)
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore(opts Options) *MemoryStore {
	id := opts.ID
	if id == "" {
		id = newID()
	}
	now := time.Now().UnixMilli()
	h := Header{Version: SessionVersion, ID: id, CreatedAt: now, CWD: opts.CWD, Name: opts.Name, ParentSessionID: opts.ParentSessionID, ParentBranchID: opts.ParentBranchID, ForkEntryID: opts.ForkEntryID}
	s := &MemoryStore{
		id:           id,
		header:       h,
		byID:         make(map[string]int),
		branches:     make(map[string]protocol.SessionBranch),
		threadModes:  map[string]protocol.CollaborationMode{"main": protocol.ModeDefault},
		threadGoals:  make(map[string]*protocol.ThreadGoal),
		goalDeferred: make(map[string]bool),
		subagents:    make(map[string]SubagentRecord),
		activeBranch: "main",
	}
	root := Entry{Type: EntryMeta, ID: "root", Key: "root", Value: id}
	s.entries = append(s.entries, root)
	s.byID["root"] = 0
	s.tip = "root"
	s.branches["main"] = protocol.SessionBranch{ID: "main", Name: "main", TipID: "root", CreatedAt: now, UpdatedAt: now, Active: true}
	return s
}

// ID implements Store.
func (s *MemoryStore) ID() string { return s.id }

// Path implements Store.
func (s *MemoryStore) Path() string { return "" }

// Header implements Store.
func (s *MemoryStore) Header() Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.header
}

// SessionTitle returns the current session-wide display title.
func (s *MemoryStore) SessionTitle() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", errors.New("session: store closed")
	}
	return s.header.Name, nil
}

// RenameSession changes the display title without moving the branch tip.
func (s *MemoryStore) RenameSession(title string) error {
	title, err := normalizeSessionTitle(title)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.header.Name = title
	return nil
}

// AppendWithInitialTitle atomically appends the first user message and assigns
// its generated title. Existing/manual titles and any prior message win.
func (s *MemoryStore) AppendWithInitialTitle(entry Entry, title string) error {
	entry = cloneEntry(entry)
	if title != "" {
		var err error
		title, err = normalizeSessionTitle(title)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = s.tip
	}
	if _, ok := s.byID[entry.ID]; ok {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if _, ok := s.byID[entry.ParentID]; !ok {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	if title != "" && s.header.Name == "" {
		hasMessage := false
		for _, existing := range s.entries {
			if existing.Type == EntryMessage {
				hasMessage = true
				break
			}
		}
		if !hasMessage {
			s.header.Name = title
		}
	}
	normalizeEntryMessage(&entry)
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	branch := s.branches[s.activeBranch]
	branch.TipID = entry.ID
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// CollaborationMode returns the active branch mode.
func (s *MemoryStore) CollaborationMode() (protocol.CollaborationMode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.ModeDefault, errors.New("session: store closed")
	}
	mode := s.threadModes[s.activeBranch]
	if mode == "" {
		mode = protocol.ModeDefault
	}
	return mode, nil
}

// SetCollaborationMode persists the active branch mode without moving its tip.
func (s *MemoryStore) SetCollaborationMode(mode protocol.CollaborationMode) error {
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.threadModes[s.activeBranch] = parsed
	return nil
}

func (s *MemoryStore) Goal() (*protocol.ThreadGoal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return s.threadGoals[s.activeBranch].Clone(), nil
}

func (s *MemoryStore) CreateGoal(goal protocol.ThreadGoal, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if old := s.threadGoals[s.activeBranch]; old != nil && !old.Status.Terminal() && !replace {
		return errors.New("session: unfinished goal already exists")
	}
	goal.SessionID, goal.BranchID = s.id, s.activeBranch
	if err := goal.Validate(); err != nil {
		return err
	}
	s.threadGoals[s.activeBranch] = goal.Clone()
	s.goalDeferred[s.activeBranch] = false
	return nil
}

func (s *MemoryStore) ReplaceGoal(expected string, goal protocol.ThreadGoal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	current := s.threadGoals[s.activeBranch]
	if expected == "" {
		if current != nil {
			return newGoalConflict("goal_id", expected, s.id, s.activeBranch, current)
		}
	} else if current == nil || current.GoalID != expected {
		return newGoalConflict("goal_id", expected, s.id, s.activeBranch, current)
	}
	goal.SessionID, goal.BranchID = s.id, s.activeBranch
	if err := goal.Validate(); err != nil {
		return err
	}
	s.threadGoals[s.activeBranch] = goal.Clone()
	s.goalDeferred[s.activeBranch] = false
	return nil
}

func (s *MemoryStore) ReviseGoal(expected, nextGoalID, objective string) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected {
		return nil, newGoalConflict("goal_id", expected, s.id, s.activeBranch, g)
	}
	copy := g.Clone()
	copy.GoalID = nextGoalID
	copy.Objective = strings.TrimSpace(objective)
	copy.Status = protocol.GoalActive
	copy.BlockedReason = ""
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	s.goalDeferred[s.activeBranch] = false
	return copy.Clone(), nil
}

func (s *MemoryStore) TransitionGoal(expected string, expectedStatus, nextStatus protocol.ThreadGoalStatus, blockedReason string, clearDeferral bool) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected || g.Status != expectedStatus {
		return nil, newGoalConflict("goal_state", expected, s.id, s.activeBranch, g)
	}
	if _, err := protocol.ParseThreadGoalStatus(string(nextStatus)); err != nil {
		return nil, err
	}
	blockedReason = strings.TrimSpace(blockedReason)
	if nextStatus == protocol.GoalBlocked && blockedReason == "" {
		return nil, errors.New("session: blocked status requires a reason")
	}
	copy := g.Clone()
	copy.Status = nextStatus
	copy.BlockedReason = ""
	if nextStatus == protocol.GoalBlocked {
		copy.BlockedReason = strings.TrimSpace(blockedReason)
	}
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	if clearDeferral {
		s.goalDeferred[s.activeBranch] = false
	}
	return copy.Clone(), nil
}

func (s *MemoryStore) UpdateGoal(expected string, objective *string, status *protocol.ThreadGoalStatus, budget *int64) (*protocol.ThreadGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		return nil, ErrNotFound
	}
	if g.GoalID != expected {
		return nil, newGoalConflict("goal_id", expected, s.id, s.activeBranch, g)
	}
	copy := g.Clone()
	if objective != nil {
		copy.Objective = strings.TrimSpace(*objective)
	}
	if status != nil {
		parsed, err := protocol.ParseThreadGoalStatus(string(*status))
		if err != nil {
			return nil, err
		}
		if parsed == protocol.GoalBlocked {
			return nil, errors.New("session: blocked status requires reason-bearing transition")
		}
		copy.Status = parsed
		if parsed != protocol.GoalBlocked {
			copy.BlockedReason = ""
		}
	}
	if budget != nil {
		if *budget <= 0 {
			return nil, errors.New("session: goal budget must be positive")
		}
		v := *budget
		copy.TokenBudget = &v
	}
	copy.UpdatedAt = time.Now().UnixMilli()
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	s.threadGoals[s.activeBranch] = copy
	return copy.Clone(), nil
}

func (s *MemoryStore) ClearGoal(expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil {
		if expected != "" {
			return newGoalConflict("goal_id", expected, s.id, s.activeBranch, nil)
		}
		return nil
	}
	if expected == "" || g.GoalID != expected {
		return newGoalConflict("goal_id", expected, s.id, s.activeBranch, g)
	}
	delete(s.threadGoals, s.activeBranch)
	delete(s.goalDeferred, s.activeBranch)
	return nil
}

func normalizedGoalCostDelta(cost *protocol.Cost) (*protocol.Cost, error) {
	if cost == nil {
		return nil, nil
	}
	copy := *cost
	copy.Currency = strings.ToUpper(strings.TrimSpace(copy.Currency))
	if copy.Currency == "" {
		copy.Currency = "USD"
	}
	for _, value := range []float64{copy.Input, copy.Output, copy.CacheRead, copy.CacheWrite, copy.Total} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errors.New("session: estimated goal cost delta must be finite and non-negative")
		}
	}
	return &copy, nil
}

func addGoalCost(costs []protocol.Cost, delta *protocol.Cost) ([]protocol.Cost, error) {
	if delta == nil {
		return costs, nil
	}
	for i := range costs {
		if !strings.EqualFold(costs[i].Currency, delta.Currency) {
			continue
		}
		values := []*float64{&costs[i].Input, &costs[i].Output, &costs[i].CacheRead, &costs[i].CacheWrite, &costs[i].Total}
		deltas := []float64{delta.Input, delta.Output, delta.CacheRead, delta.CacheWrite, delta.Total}
		for j := range values {
			sum := *values[j] + deltas[j]
			if math.IsNaN(sum) || math.IsInf(sum, 0) {
				return nil, errors.New("session: estimated goal cost overflow")
			}
			*values[j] = sum
		}
		return costs, nil
	}
	return append(costs, *delta), nil
}

func (s *MemoryStore) AccountGoal(expected string, tokens, seconds int64, estimatedCostDelta *protocol.Cost) (*protocol.ThreadGoal, bool, error) {
	if tokens < 0 || seconds < 0 {
		return nil, false, errors.New("session: goal usage delta cannot be negative")
	}
	cost, err := normalizedGoalCostDelta(estimatedCostDelta)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.threadGoals[s.activeBranch]
	if g == nil || g.GoalID != expected {
		return g.Clone(), false, newGoalConflict("goal_id", expected, s.id, s.activeBranch, g)
	}
	if tokens > math.MaxInt64-g.TokensUsed || seconds > math.MaxInt64-g.SecondsUsed {
		return nil, false, errors.New("session: goal usage overflow")
	}
	copy := g.Clone()
	copy.TokensUsed += tokens
	copy.SecondsUsed += seconds
	copy.EstimatedCosts, err = addGoalCost(copy.EstimatedCosts, cost)
	if err != nil {
		return nil, false, err
	}
	copy.UpdatedAt = time.Now().UnixMilli()
	crossed := false
	if copy.Status == protocol.GoalActive && copy.TokenBudget != nil && copy.TokensUsed >= *copy.TokenBudget {
		copy.Status = protocol.GoalBudgetLimited
		crossed = true
	}
	s.threadGoals[s.activeBranch] = copy
	return copy.Clone(), crossed, nil
}

func (s *MemoryStore) GoalContinuationDeferred() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.goalDeferred[s.activeBranch], nil
}

func (s *MemoryStore) SetGoalContinuationDeferred(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	s.goalDeferred[s.activeBranch] = v
	return nil
}

// Metadata returns the latest value for key in the session.
func (s *MemoryStore) Metadata(key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false, errors.New("session: store closed")
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		if entry := s.entries[i]; entry.Type == EntryMeta && entry.Key == key {
			return entry.Value, true, nil
		}
	}
	return "", false, nil
}

// SetMetadata appends a metadata entry to the active branch.
func (s *MemoryStore) SetMetadata(key, value string) error {
	return s.Append(Entry{Type: EntryMeta, Key: key, Value: value})
}

// Append implements Store.
func (s *MemoryStore) Append(entry Entry) error {
	entry = cloneEntry(entry)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.ParentID == "" {
		entry.ParentID = s.tip
	}
	if _, ok := s.byID[entry.ID]; ok {
		return fmt.Errorf("session: duplicate entry id %q", entry.ID)
	}
	if _, ok := s.byID[entry.ParentID]; !ok {
		return fmt.Errorf("session: unknown parent %q", entry.ParentID)
	}
	normalizeEntryMessage(&entry)
	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = len(s.entries) - 1
	s.tip = entry.ID
	branch := s.branches[s.activeBranch]
	branch.TipID = entry.ID
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// AppendBatch atomically appends an ordered chain and advances the branch once.
func (s *MemoryStore) AppendBatch(batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}
	batch = cloneEntries(batch)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	parent := s.tip
	seen := map[string]bool{}
	for i := range batch {
		if batch[i].ID == "" {
			batch[i].ID = newID()
		}
		if batch[i].ParentID == "" {
			batch[i].ParentID = parent
		}
		if batch[i].ParentID != parent {
			return errors.New("session: batch is not a linear chain")
		}
		if _, ok := s.byID[batch[i].ID]; ok || seen[batch[i].ID] {
			return fmt.Errorf("session: duplicate entry id %q", batch[i].ID)
		}
		if i == 0 {
			if _, ok := s.byID[batch[i].ParentID]; !ok {
				return fmt.Errorf("session: unknown parent %q", batch[i].ParentID)
			}
		}
		normalizeEntryMessage(&batch[i])
		seen[batch[i].ID] = true
		parent = batch[i].ID
	}
	for _, entry := range batch {
		s.entries = append(s.entries, entry)
		s.byID[entry.ID] = len(s.entries) - 1
	}
	s.tip = parent
	branch := s.branches[s.activeBranch]
	branch.TipID = parent
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// BranchTip implements Store.
func (s *MemoryStore) BranchTip() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tip
}

// SetBranchTip implements Store.
func (s *MemoryStore) SetBranchTip(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	s.tip = id
	branch := s.branches[s.activeBranch]
	branch.TipID = id
	branch.UpdatedAt = time.Now().UnixMilli()
	s.branches[s.activeBranch] = branch
	return nil
}

// Messages implements Store.
func (s *MemoryStore) Messages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return linearize(s.entries, s.byID, s.tip)
}

// TailMessages implements TailMessageStore without constructing the complete
// root-to-tip path.
func (s *MemoryStore) TailMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	id := s.tip
	reversed := make([]protocol.Message, 0, 4)
	for id != "" {
		index, ok := s.byID[id]
		if !ok {
			return nil, ErrNotFound
		}
		entry := s.entries[index]
		if entry.Type == EntryMessage && entry.Message != nil {
			message := entry.Message.Clone()
			reversed = append(reversed, message)
			if message.Role != protocol.RoleTool {
				break
			}
		}
		id = entry.ParentID
	}
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return reversed, nil
}

// LatestAssistantMessage implements LatestAssistantStore with a parent-index
// walk and clones only the selected result.
func (s *MemoryStore) LatestAssistantMessage() (protocol.Message, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.Message{}, false, errors.New("session: store closed")
	}
	current := s.tip
	for steps := 0; current != "" && steps <= len(s.entries); steps++ {
		index, ok := s.byID[current]
		if !ok {
			break
		}
		entry := s.entries[index]
		if entry.Type == EntryMessage && entry.Message != nil && entry.Message.Role == protocol.RoleAssistant && assistantHasResult(*entry.Message) {
			return entry.Message.Clone(), true, nil
		}
		current = entry.ParentID
	}
	return protocol.Message{}, false, nil
}

// AgentRunStats returns durable turn/step counts on the active branch.
func (s *MemoryStore) AgentRunStats() (AgentRunStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return AgentRunStats{}, errors.New("session: store closed")
	}
	return agentRunStatsFromEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// CountAgentTurns retains the legacy count-only interface.
func (s *MemoryStore) CountAgentTurns() (uint64, error) {
	stats, err := s.AgentRunStats()
	return stats.Turns, err
}

// AggregateUsage sums the active branch without constructing message clones.
func (s *MemoryStore) AggregateUsage() (protocol.Usage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return protocol.Usage{}, errors.New("session: store closed")
	}
	var total protocol.Usage
	current := s.tip
	for steps := 0; current != "" && steps <= len(s.entries); steps++ {
		index, ok := s.byID[current]
		if !ok {
			break
		}
		entry := s.entries[index]
		if entry.Type == EntryMessage && entry.Message != nil && entry.Message.Usage != nil {
			total = total.Add(*entry.Message.Usage)
		} else if entry.Type == EntryMeta && entry.Key == MetaProviderUsage {
			var usage protocol.Usage
			if err := json.Unmarshal([]byte(entry.Value), &usage); err != nil {
				return protocol.Usage{}, err
			}
			total = total.Add(usage)
		}
		current = entry.ParentID
	}
	return total, nil
}

func assistantHasResult(message protocol.Message) bool {
	for _, block := range message.Content {
		if (block.Type == protocol.BlockText || block.Type == protocol.BlockPlan) && strings.TrimSpace(block.Text) != "" {
			return true
		}
	}
	return false
}

// ContextMessages implements ContextStore.
func (s *MemoryStore) ContextMessages() ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return contextMessagesFromEntries(contextPathFrom(s.entries, s.byID, s.tip)), nil
}

// contextPathFrom walks only as far as the nearest effective compaction
// boundary. Exact entries remain retained in MemoryStore, but recurring
// provider projections no longer rebuild the complete pre-compaction path.
func contextPathFrom(entries []Entry, byID map[string]int, tip string) []Entry {
	reversed := make([]Entry, 0, contextProjectionChunkMessages)
	seen := make(map[string]struct{})
	current := tip
	markerAt := -1
	boundaryID := ""
	boundaryFound := false
	for current != "" {
		if _, duplicate := seen[current]; duplicate {
			break
		}
		seen[current] = struct{}{}
		index, ok := byID[current]
		if !ok {
			break
		}
		entry := entries[index]
		reversed = append(reversed, entry)
		if markerAt < 0 && entry.Type == EntryCompaction && strings.TrimSpace(entry.Summary) != "" {
			markerAt = len(reversed) - 1
			boundaryID = entry.CompactedThrough
			if boundaryID == "" {
				break
			}
		} else if markerAt >= 0 && entry.ID == boundaryID {
			boundaryFound = true
			break
		}
		current = entry.ParentID
	}
	if markerAt >= 0 && boundaryID != "" && !boundaryFound {
		// Unknown, forward, or off-branch boundaries clamp at the marker exactly
		// like SQLite's context projection; older history must not resurface.
		reversed = reversed[:markerAt+1]
	}
	out := make([]Entry, len(reversed))
	for i := range reversed {
		out[len(reversed)-1-i] = reversed[i]
	}
	return out
}

// BranchEntries implements BranchEntryStore.
func (s *MemoryStore) BranchEntries() ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return cloneEntries(pathFrom(s.entries, s.byID, s.tip)), nil
}

// BranchStateEntries implements BranchStateStore without materializing the
// complete branch. Append validation guarantees an acyclic parent chain; the
// entry-count guard remains defensive against malformed in-memory fixtures.
func (s *MemoryStore) BranchStateEntries(metaKeys, toolNames []string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	wantedMeta := makeStringSet(metaKeys)
	wantedTools := makeStringSet(toolNames)
	reversed := make([]Entry, 0)
	current := s.tip
	for steps := 0; current != "" && steps <= len(s.entries); steps++ {
		index, ok := s.byID[current]
		if !ok {
			break
		}
		entry := s.entries[index]
		if branchStateEntryMatches(entry, wantedMeta, wantedTools) {
			reversed = append(reversed, cloneEntry(entry))
		}
		current = entry.ParentID
	}
	out := make([]Entry, len(reversed))
	for i := range reversed {
		out[len(reversed)-1-i] = reversed[i]
	}
	return out, nil
}

func makeStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func branchStateEntryMatches(entry Entry, metaKeys, toolNames map[string]struct{}) bool {
	if entry.Type == EntryMeta {
		_, ok := metaKeys[entry.Key]
		return ok
	}
	if entry.Type != EntryMessage || entry.Message == nil || entry.Message.Role != protocol.RoleTool {
		return false
	}
	_, ok := toolNames[entry.Message.ToolName]
	return ok
}

// Branches implements BranchStore.
func (s *MemoryStore) Branches() ([]protocol.SessionBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	out := make([]protocol.SessionBranch, 0, len(s.branches))
	for _, branch := range s.branches {
		path := pathFrom(s.entries, s.byID, branch.TipID)
		branch.Messages, branch.Preview = branchStats(path)
		branch.Active = branch.ID == s.activeBranch
		out = append(out, branch)
	}
	slices.SortFunc(out, func(a, b protocol.SessionBranch) int {
		if byCreated := cmp.Compare(a.CreatedAt, b.CreatedAt); byCreated != 0 {
			return byCreated
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

// SelectBranch implements BranchStore.
func (s *MemoryStore) SelectBranch(branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	branch, ok := s.branches[branchID]
	if !ok {
		return ErrNotFound
	}
	for id, current := range s.branches {
		current.Active = id == branchID
		s.branches[id] = current
	}
	s.activeBranch = branchID
	s.tip = branch.TipID
	return nil
}

// ForkBranch implements BranchStore. The new branch shares the same entry tree.
func (s *MemoryStore) ForkBranch(fromEntryID string) (protocol.SessionBranch, error) {
	return s.ForkBranchWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}
