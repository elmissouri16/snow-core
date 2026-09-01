package app

import "testing"

func TestContextAndSkillFacadesUseRootAgent(t *testing.T) {
	a, err := New(t.Context(), Options{
		Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	report, err := a.ContextReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Categories) == 0 || report.EstimatedInputTokens == 0 {
		t.Fatalf("context report = %+v", report)
	}
	cleared, err := a.ClearActiveSkills()
	if err != nil || cleared != 0 {
		t.Fatalf("ClearActiveSkills() = %d, %v", cleared, err)
	}
}

func TestContextAndSkillFacadesRejectMissingAgent(t *testing.T) {
	var a *App
	if _, err := a.ContextReport(); err == nil {
		t.Fatal("ContextReport accepted nil app")
	}
	if _, err := a.ClearActiveSkills(); err == nil {
		t.Fatal("ClearActiveSkills accepted nil app")
	}
}
