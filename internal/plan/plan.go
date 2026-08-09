// Package plan contains Plan-mode instructions and the incremental proposed-plan parser.
package plan

import "strings"

const (
	OpenTag  = "<proposed_plan>"
	CloseTag = "</proposed_plan>"
)

// Instructions are appended to every provider request in Plan mode.
const Instructions = `# Plan Mode (Conversational)

You work in three phases and chat your way to a decision-complete plan before finalizing it. You remain in Plan Mode until a developer/system instruction ends it. User imperative language does not end the mode: requests to execute mean plan the execution, not perform it.

Plan Mode and update_plan are different. update_plan is a Default-mode TODO/checklist tool and is unavailable here.

## Execution versus mutation
You may use non-mutating actions that gather truth, reduce ambiguity, or validate feasibility: read/search files, inspect types/configuration/docs, perform static analysis, and run genuinely non-mutating checks or tests. You must not edit or write files, run rewriting formatters/code generators/migrations, apply patches, or perform side effects whose purpose is implementing the plan. This is an instruction boundary, not a sandbox; when in doubt, do not mutate.

## Phase 1 — Ground in the environment
Explore first and ask second. Resolve discoverable facts from the repository/system with targeted non-mutating inspection before asking the user. Ask before exploring only for an obvious contradiction in the request itself.

## Phase 2 — Intent chat
Clarify goal and success criteria, audience, scope, constraints, current state, and material preferences/tradeoffs. Do not finalize while a high-impact intent ambiguity remains.

## Phase 3 — Implementation chat
Once intent is stable, make the specification decision-complete: approach, public interfaces and data flow, edge/failure cases, compatibility/migration needs, tests and acceptance criteria, and rollout/monitoring where relevant.

## Questions
Prefer request_user_input. Ask only questions that materially change the plan, confirm an important assumption, choose a meaningful tradeoff, or request information that cannot be discovered. Offer 2–4 meaningful mutually exclusive choices and a recommended default when possible. Never ask the user for facts available through repository/system inspection.

## Finalization
Only present an official plan when it leaves no decisions to the implementer. Emit at most one official plan block per turn. Put the exact opening and closing tags on their own lines, with Markdown between them:

<proposed_plan>
plan content
</proposed_plan>

The plan must have a clear title, brief summary, important public API/interface/type changes, tests/scenarios, and explicit assumptions/defaults where needed. Prefer compact behavior-oriented sections over exhaustive file inventories. Do not ask “should I proceed?”; after an official plan the user can switch to Default mode. If revising a prior plan, a new block is a complete replacement. If there is not enough information for a complete replacement, continue planning without a block.`

// SegmentKind identifies output extracted from streamed assistant text.
type SegmentKind uint8

const (
	Normal SegmentKind = iota
	ProposedPlanStart
	ProposedPlanDelta
	ProposedPlanEnd
)

// Segment preserves the order of normal and plan output.
type Segment struct {
	Kind SegmentKind
	Text string
}

// Parser incrementally parses line-delimited proposed-plan tags. It recognizes
// only the first block; later/stray tags remain ordinary visible text. It never
// buffers an ordinary line: only a possible exact tag prefix at line start is
// retained, bounding withheld output to len(CloseTag)+1 bytes.
type Parser struct {
	pending     string
	lineStart   bool
	initialized bool
	inPlan      bool
	sawPlan     bool
	finished    bool
}

func (p *Parser) Push(chunk string) []Segment {
	if p.finished || chunk == "" {
		return nil
	}
	if !p.initialized {
		// The zero value starts at a line boundary.
		p.lineStart = true
		p.initialized = true
	}
	var out []Segment
	for i := 0; i < len(chunk); i++ {
		part := chunk[i : i+1]
		candidate := p.candidate()
		if !p.lineStart || candidate == "" {
			if newline := strings.IndexByte(chunk[i:], '\n'); newline >= 0 {
				out = append(out, p.text(chunk[i:i+newline+1]))
				p.lineStart = true
				i += newline
				continue
			}
			out = append(out, p.text(chunk[i:]))
			p.lineStart = false
			break
		}
		if p.lineStart {
			p.pending += part
			switch {
			case p.pending == candidate+"\n" || p.pending == candidate+"\r\n":
				out = append(out, p.recognize(candidate)...)
				p.pending = ""
				p.lineStart = true
			case p.pending == candidate, p.pending == candidate+"\r", strings.HasPrefix(candidate, p.pending):
				// Withhold only while this can still be an exact line tag.
			default:
				out = append(out, p.text(p.pending))
				p.lineStart = strings.HasSuffix(p.pending, "\n")
				p.pending = ""
			}
			continue
		}
	}
	return merge(out)
}

// Finish flushes a successful response. An exact tag at EOF is recognized and
// an unterminated first plan is synthetically closed by contract.
func (p *Parser) Finish() []Segment { return p.finish(false) }

// Interrupt flushes bytes from a cancelled or failed response without
// completing its plan. Exact recognized tags remain hidden, but partial tag
// prefixes are returned as visible text in their current segment.
func (p *Parser) Interrupt() []Segment { return p.finish(true) }

func (p *Parser) finish(interrupted bool) []Segment {
	if p.finished {
		return nil
	}
	p.finished = true
	var out []Segment
	candidate := p.candidate()
	pending := p.pending
	p.pending = ""
	trimmed := strings.TrimSuffix(pending, "\r")
	if candidate != "" && trimmed == candidate {
		if candidate == OpenTag {
			out = append(out, p.recognize(candidate)...)
		} else if !interrupted {
			out = append(out, p.recognize(candidate)...)
		}
	} else if pending != "" {
		out = append(out, p.text(pending))
	}
	if p.inPlan && !interrupted {
		p.inPlan = false
		out = append(out, Segment{Kind: ProposedPlanEnd})
	}
	return merge(out)
}

func (p *Parser) candidate() string {
	if !p.lineStart && p.pending == "" {
		return ""
	}
	if p.inPlan {
		return CloseTag
	}
	if !p.sawPlan {
		return OpenTag
	}
	return ""
}

func (p *Parser) recognize(candidate string) []Segment {
	if candidate == OpenTag {
		p.inPlan = true
		p.sawPlan = true
		return []Segment{{Kind: ProposedPlanStart}}
	}
	p.inPlan = false
	return []Segment{{Kind: ProposedPlanEnd}}
}

func (p *Parser) text(text string) Segment {
	kind := Normal
	if p.inPlan {
		kind = ProposedPlanDelta
	}
	return Segment{Kind: kind, Text: text}
}

func merge(in []Segment) []Segment {
	out := make([]Segment, 0, len(in))
	for _, segment := range in {
		if segment.Text == "" && (segment.Kind == Normal || segment.Kind == ProposedPlanDelta) {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Kind == segment.Kind && (segment.Kind == Normal || segment.Kind == ProposedPlanDelta) {
			out[len(out)-1].Text += segment.Text
			continue
		}
		out = append(out, segment)
	}
	return out
}

// Extract returns normal text and the first proposed plan from a complete response.
func Extract(text string) (normal, proposed string, found bool) {
	var parser Parser
	segments := append(parser.Push(text), parser.Finish()...)
	var normalOut, planOut strings.Builder
	for _, segment := range segments {
		switch segment.Kind {
		case Normal:
			normalOut.WriteString(segment.Text)
		case ProposedPlanStart:
			found = true
		case ProposedPlanDelta:
			planOut.WriteString(segment.Text)
		}
	}
	return normalOut.String(), planOut.String(), found
}
