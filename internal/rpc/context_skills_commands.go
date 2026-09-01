package rpc

import (
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *Server) handleContextReport(req Request) error {
	if len(req.Params) != 0 {
		return errors.New("context does not accept params")
	}
	if req.Secret != "" {
		return errors.New("context does not accept secret")
	}
	report, err := s.app.ContextReport()
	if err != nil {
		return err
	}
	categories := make([]protocol.RPCContextCategory, len(report.Categories))
	for i, category := range report.Categories {
		categories[i] = protocol.RPCContextCategory{
			Name: category.Name, Bytes: category.Bytes,
			EstimatedTokens: category.EstimatedTokens, Items: category.Items,
		}
	}
	result := protocol.RPCContextReport{
		LatestRequest: report.LatestRequest, Categories: categories,
		EstimatedInputTokens:     report.EstimatedInputTokens,
		FixedContextTokens:       report.FixedContextTokens,
		FixedContextBudgetTokens: report.FixedContextBudgetTokens,
		FixedContextOverBudget:   report.FixedContextOverBudget,
		MessageCount:             report.MessageCount, ToolCount: report.ToolCount,
		ContextWindow: report.ContextWindow, Usage: report.Usage.Clone(),
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}

func (s *Server) handleSkillsClear(req Request) error {
	if len(req.Params) != 0 {
		return errors.New("skills_clear does not accept params")
	}
	if req.Secret != "" {
		return errors.New("skills_clear does not accept secret")
	}
	catalog, err := s.app.SkillInventoryPublic()
	if err != nil {
		return err
	}
	cleared, err := s.app.ClearActiveSkills()
	if err != nil {
		return err
	}
	return s.write(Response{
		ID: req.ID, Type: "response", Command: req.Type, Success: true,
		Data: protocol.RPCSkillsClearResult{Cleared: cleared, Catalog: catalog},
	})
}
