package session

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAgentRunStatsCombinesLegacyPrefixWithExplicitMarkers(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			legacyUser := protocol.NewUserMessage("legacy-user", "", "start")
			legacyToolStep := protocol.NewAssistantMessage("legacy-tool-step", legacyUser.ID, "p", "m", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"}}, protocol.StopToolUse, nil)
			legacyToolResult := protocol.NewToolResultMessage("legacy-result", legacyToolStep.ID, "call", "read", []protocol.ContentBlock{protocol.NewTextBlock("ok")}, false)
			legacyFinal := protocol.NewAssistantMessage("legacy-final", legacyToolResult.ID, "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
			legacyGoalFinal := protocol.NewAssistantMessage("legacy-goal-final", legacyFinal.ID, "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("continued")}, protocol.StopStop, nil)
			for _, message := range []protocol.Message{legacyUser, legacyToolStep, legacyToolResult, legacyFinal, legacyGoalFinal} {
				if err := store.Append(Entry{Type: EntryMessage, ID: message.ID, Message: &message}); err != nil {
					t.Fatal(err)
				}
			}
			legacyTip := store.BranchTip()
			assertRunStats(t, store, AgentRunStats{Turns: 1, Steps: 3})

			for _, entry := range []Entry{
				{Type: EntryMeta, ID: "explicit-turn", Key: MetaAgentTurn, Value: "user"},
				{Type: EntryMeta, ID: "explicit-step", Key: MetaAgentStep, Value: "provider"},
			} {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			explicitFinal := protocol.NewAssistantMessage("explicit-final", "explicit-step", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("new")}, protocol.StopStop, nil)
			if err := store.Append(Entry{Type: EntryMessage, ID: explicitFinal.ID, Message: &explicitFinal}); err != nil {
				t.Fatal(err)
			}
			assertRunStats(t, store, AgentRunStats{Turns: 2, Steps: 4})
			snapshot, err := store.(BranchHydrationStore).BranchHydration()
			if err != nil || snapshot.TurnCount != 2 || snapshot.StepCount != 4 {
				t.Fatalf("hydration stats turns=%d steps=%d err=%v", snapshot.TurnCount, snapshot.StepCount, err)
			}

			if err := store.SetBranchTip(legacyTip); err != nil {
				t.Fatal(err)
			}
			assertRunStats(t, store, AgentRunStats{Turns: 1, Steps: 3})
		})
	}
}

func TestAgentRunStatsFollowBranchAncestry(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			for _, entry := range []Entry{
				{Type: EntryMeta, ID: "turn-root", Key: MetaAgentTurn, Value: "user"},
				{Type: EntryMeta, ID: "step-root", Key: MetaAgentStep, Value: "provider"},
			} {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			branch, err := store.(BranchStore).ForkBranch("step-root")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.(BranchStore).SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			if err := store.Append(Entry{Type: EntryMeta, ID: "step-main", Key: MetaAgentStep, Value: "provider"}); err != nil {
				t.Fatal(err)
			}
			assertRunStats(t, store, AgentRunStats{Turns: 1, Steps: 2})

			if err := store.(BranchStore).SelectBranch(branch.ID); err != nil {
				t.Fatal(err)
			}
			assertRunStats(t, store, AgentRunStats{Turns: 1, Steps: 1})
			if err := store.Append(Entry{Type: EntryMeta, ID: "turn-child", Key: MetaAgentTurn, Value: "goal"}); err != nil {
				t.Fatal(err)
			}
			assertRunStats(t, store, AgentRunStats{Turns: 2, Steps: 1})
		})
	}
}

func TestAgentRunStatsIgnoreInvalidMarkers(t *testing.T) {
	entries := []Entry{
		{Type: EntryMeta, ID: "bad-turn", Key: MetaAgentTurn, Value: "compact"},
		{Type: EntryMeta, ID: "bad-step", Key: MetaAgentStep, Value: "retry"},
		{Type: EntryMeta, ID: "step", Key: MetaAgentStep, Value: "provider"},
	}
	got := agentRunStatsFromEntries(entries)
	if got != (AgentRunStats{Steps: 1}) {
		t.Fatalf("stats=%+v", got)
	}
}

func assertRunStats(t *testing.T, store Store, want AgentRunStats) {
	t.Helper()
	got, err := store.(AgentRunStatsStore).AgentRunStats()
	if err != nil || got != want {
		t.Fatalf("run stats=%+v want=%+v err=%v", got, want, err)
	}
}
