// Package plan contains Plan-mode instructions and the incremental proposed-plan parser.
package plan

import (
	_ "embed"
	"strings"
)

const (
	OpenTag  = "<proposed_plan>"
	CloseTag = "</proposed_plan>"
)

//go:embed system.md
var instructionsMarkdown string

// Instructions are appended to every provider request in Plan mode. The
// Markdown source stays editable while go:embed preserves the single binary.
var Instructions = strings.TrimSuffix(instructionsMarkdown, "\n")

// DefaultInstructions makes the inactive Plan-mode boundary explicit. Without
// it, old transcript claims about Plan mode can look current after a switch.
const DefaultInstructions = `# Default Mode

You are in Default collaboration mode. Plan Mode is not active. You may execute user requests with the available tools, subject to all other instructions and permission gates. Active Agent Skills are independent constraints; if one prevents requested work, identify that skill rather than claiming Plan Mode is active.`

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
