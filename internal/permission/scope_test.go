package permission

import "testing"

func TestCachedAllowCannotOverrideNonRememberableAnalysis(t *testing.T) {
	req := Request{Tool: "bash", Risk: RiskExec, ScopeKey: "scope", Rememberable: false, Unknown: true}
	if _, ok := rememberedDecision(map[string]Decision{ruleKey(req): DecisionAllow}, req); ok {
		t.Fatal("cached allow overrode current uncertainty")
	}
	if got, ok := rememberedDecision(map[string]Decision{ruleKey(req): DecisionDeny}, req); !ok || got != DecisionDeny {
		t.Fatal("conservative cached denial was lost")
	}
}
