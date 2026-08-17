package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestContextCommandReportsProjectedCategories(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, cmd := m.runCommand("/context")
	if cmd == nil {
		t.Fatal("/context did not schedule the context scan")
	}
	msg, ok := cmd().(contextReportMsg)
	if !ok {
		t.Fatalf("/context command returned %T", msg)
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	_, _ = m.Update(msg)
	output := stripANSI(strings.Join(m.lines, "\n"))
	for _, want := range []string{
		"Context report · stored context preflight",
		"Current context",
		"System prompt",
		"Tool schemas",
		"Context total",
		"Estimated composition",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("/context output missing %q:\n%s", want, output)
		}
	}
}

func TestContextCommandRejectsArguments(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, cmd := m.runCommand("/context extra")
	if cmd != nil {
		t.Fatal("invalid /context unexpectedly scheduled work")
	}
	output := stripANSI(strings.Join(m.lines, "\n"))
	if !strings.Contains(output, "/context takes no arguments") {
		t.Fatalf("invalid /context output = %q", output)
	}
}

func TestContextReportDropsStaleBranchResult(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	before := len(m.lines)
	_, _ = m.Update(contextReportMsg{
		epoch:  m.app.Agent.RootEpoch() + 1,
		report: agent.ContextReport{EstimatedInputTokens: 10},
	})
	if len(m.lines) != before {
		t.Fatalf("stale context report was rendered: %v", m.lines[before:])
	}
}

func TestFormatContextReportUsesEstimateWhenInputUsageIsMissing(t *testing.T) {
	report := agent.ContextReport{
		LatestRequest:        true,
		EstimatedInputTokens: 100,
		ContextWindow:        1000,
		Usage:                &protocol.Usage{Output: 25},
	}
	output := formatContextReport(report, 0, false)
	if !strings.Contains(output, "Current context         ~100 / 1k (10.0%)") {
		t.Fatalf("missing-input usage did not retain estimate:\n%s", output)
	}
	if strings.Contains(output, "Latest provider input") {
		t.Fatalf("missing-input usage was presented as authoritative:\n%s", output)
	}
}

func TestFormatContextReportCalibratesPreflightToFooterContext(t *testing.T) {
	report := agent.ContextReport{
		EstimatedInputTokens: 161000,
		ContextWindow:        272000,
		Categories: []agent.ContextCategory{
			{Name: "System prompt", EstimatedTokens: 9400},
			{Name: "Tool results", EstimatedTokens: 76600},
			{Name: "Provider state", EstimatedTokens: 52800},
			{Name: "Images", EstimatedTokens: 22200, Items: 15},
		},
	}
	output := formatContextReport(report, 112800, false)
	for _, want := range []string{
		"provider-calibrated",
		"Current context         112.8k / 272k (41.5%)",
		"Context total             112.8k",
		"Images (15)",
		"Raw local estimate",
		"~    161k",
		"before calibration",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("calibrated preflight missing %q:\n%s", want, output)
		}
	}
	categories := calibrateContextCategories(report.Categories, report.EstimatedInputTokens, 112800)
	total := 0
	for _, category := range categories {
		total += category.tokens
	}
	if total != 112800 {
		t.Fatalf("calibrated categories total = %d, want 112800", total)
	}
}

func TestFormatContextReportShowsProviderMeasurementAndLatestOutput(t *testing.T) {
	report := agent.ContextReport{
		LatestRequest:        true,
		EstimatedInputTokens: 100,
		MessageCount:         3,
		ToolCount:            2,
		ContextWindow:        1000,
		Categories: []agent.ContextCategory{
			{Name: "System prompt", EstimatedTokens: 40},
			{Name: "Tool results", EstimatedTokens: 60},
		},
		Usage: &protocol.Usage{Input: 120, Output: 25, Reasoning: 5},
	}
	output := formatContextReport(report, 145, false)
	for _, want := range []string{
		"latest provider request + generated content",
		"Current context         145 / 1k (14.5%)",
		"Latest provider input   120",
		"Generated since input",
		"Raw local estimate",
		"(before calibration)",
		"25 output · 5 reasoning",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("context report missing %q:\n%s", want, output)
		}
	}
}
