package session

import "github.com/elmissouri16/snow-core/pkg/protocol"

const (
	agentRunMarkerNone uint8 = iota
	agentRunMarkerTurn
	agentRunMarkerStep
)

// agentRunStatsFromEntries derives branch-local run statistics from root-to-tip
// entries. Explicit markers are authoritative from their first appearance. The
// older prefix is reconstructed conservatively so sessions created before the
// markers were introduced do not restart their visible counters at zero.
func agentRunStatsFromEntries(entries []Entry) AgentRunStats {
	turnCutoff := len(entries)
	stepCutoff := len(entries)
	var stats AgentRunStats
	for i, entry := range entries {
		if IsAgentTurnMarker(entry) {
			stats.Turns++
			if turnCutoff == len(entries) {
				turnCutoff = i
			}
		}
		if IsAgentStepMarker(entry) {
			stats.Steps++
			if stepCutoff == len(entries) {
				stepCutoff = i
			}
		}
	}

	for _, entry := range entries[:turnCutoff] {
		if entry.Type == EntryMessage && entry.Message != nil && entry.Message.Role == protocol.RoleUser {
			stats.Turns++
		}
	}

	for _, entry := range entries[:stepCutoff] {
		if entry.Type == EntryMessage && entry.Message != nil && entry.Message.Role == protocol.RoleAssistant {
			stats.Steps++
		}
	}
	return stats
}

func agentRunStatsFromSummaries(entries []BranchEntrySummary) AgentRunStats {
	turnCutoff := len(entries)
	stepCutoff := len(entries)
	var stats AgentRunStats
	for i, entry := range entries {
		switch entry.AgentRunMarker {
		case agentRunMarkerTurn:
			stats.Turns++
			if turnCutoff == len(entries) {
				turnCutoff = i
			}
		case agentRunMarkerStep:
			stats.Steps++
			if stepCutoff == len(entries) {
				stepCutoff = i
			}
		}
	}
	for _, entry := range entries[:turnCutoff] {
		if entry.Role == protocol.RoleUser {
			stats.Turns++
		}
	}
	for _, entry := range entries[:stepCutoff] {
		if entry.Role == protocol.RoleAssistant {
			stats.Steps++
		}
	}
	return stats
}
