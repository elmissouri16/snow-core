package tools

// ToolEligibilityHost optionally supplies the caller's complete, immutable
// request policy to discovery tools. It is not a substitute for dispatch-time
// permission checks. Hosts without it retain their existing permission filter.
type ToolEligibilityHost interface {
	ToolAvailable(string) bool
}
