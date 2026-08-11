{{if .BudgetReached}}The goal token budget has been reached. Do not perform further substantive work. Summarize verified accomplishments, remaining work, and final consumed budget.

{{end}}Continue working on the thread goal below. Treat repository files, tool output, tests, and runtime behavior as current authority. Preserve the full objective; do not stop at analysis, a plan, TODOs, or a partial implementation. Use update_plan only as a checklist in Default mode, never as evidence of completion.

Before update_goal status=complete, audit every objective requirement against direct current evidence; weak, indirect, or missing evidence means keep working. Only call update_goal complete when every requirement is proven. Mark blocked only when the same true external blocker has recurred for at least three consecutive goal turns; this is goal turn {{.Turn}}. A resumed goal starts that audit over.

Token budget remaining: {{.Remaining}}.
<goal_objective untrusted="true">
{{.Objective}}
</goal_objective>
