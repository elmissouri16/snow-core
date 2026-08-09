package plan

import (
	"strings"
	"testing"
)

func collect(chunks ...string) []Segment {
	var parser Parser
	var out []Segment
	for _, chunk := range chunks {
		out = append(out, parser.Push(chunk)...)
	}
	return append(out, parser.Finish()...)
}

func flatten(segments []Segment) (normal, proposed string, starts, ends int) {
	var n, p strings.Builder
	for _, segment := range segments {
		switch segment.Kind {
		case Normal:
			n.WriteString(segment.Text)
		case ProposedPlanDelta:
			p.WriteString(segment.Text)
		case ProposedPlanStart:
			starts++
		case ProposedPlanEnd:
			ends++
		}
	}
	return n.String(), p.String(), starts, ends
}

func TestParserEverySplitPoint(t *testing.T) {
	input := "Intro\n<proposed_plan>\n# Héllo\n- step\n</proposed_plan>\nOutro"
	for split := 0; split <= len(input); split++ {
		normal, proposed, starts, ends := flatten(collect(input[:split], input[split:]))
		if normal != "Intro\nOutro" || proposed != "# Héllo\n- step\n" || starts != 1 || ends != 1 {
			t.Fatalf("split %d: normal=%q plan=%q starts=%d ends=%d", split, normal, proposed, starts, ends)
		}
	}
}

func TestParserPlanOnlyCRLFAndUnterminated(t *testing.T) {
	normal, proposed, starts, ends := flatten(collect("<proposed_plan>\r\n- one\r\n"))
	if normal != "" || proposed != "- one\r\n" || starts != 1 || ends != 1 {
		t.Fatalf("normal=%q plan=%q starts=%d ends=%d", normal, proposed, starts, ends)
	}
}

func TestParserDuplicateAndStrayTagsRemainVisible(t *testing.T) {
	input := "</proposed_plan>\n<proposed_plan>\nfirst\n</proposed_plan>\n<proposed_plan>\nsecond\n</proposed_plan>\n"
	normal, proposed, starts, ends := flatten(collect(input))
	if proposed != "first\n" || starts != 1 || ends != 1 {
		t.Fatalf("plan=%q starts=%d ends=%d", proposed, starts, ends)
	}
	for _, want := range []string{"</proposed_plan>\n", "<proposed_plan>\nsecond\n</proposed_plan>\n"} {
		if !strings.Contains(normal, want) {
			t.Fatalf("normal %q missing %q", normal, want)
		}
	}
}

func TestParserMalformedTagsRemainVisible(t *testing.T) {
	input := " <proposed_plan>\n<proposed_plan> extra\ntext"
	normal, proposed, starts, ends := flatten(collect(input))
	if normal != input || proposed != "" || starts != 0 || ends != 0 {
		t.Fatalf("normal=%q plan=%q starts=%d ends=%d", normal, proposed, starts, ends)
	}
}

func TestParserPushEmitsOrdinaryTextWithoutWaitingForNewline(t *testing.T) {
	var parser Parser
	segments := parser.Push("ordinary streamed text")
	normal, proposed, starts, ends := flatten(segments)
	if normal != "ordinary streamed text" || proposed != "" || starts != 0 || ends != 0 || parser.pending != "" {
		t.Fatalf("segments=%+v pending=%q", segments, parser.pending)
	}

	var candidate Parser
	if got := candidate.Push("<proposed_"); len(got) != 0 {
		t.Fatalf("possible tag leaked early: %+v", got)
	}
	segments = candidate.Push("x and the rest")
	normal, _, _, _ = flatten(segments)
	if normal != "<proposed_x and the rest" || candidate.pending != "" {
		t.Fatalf("normal=%q pending=%q", normal, candidate.pending)
	}
}

func TestParserLongLineHasBoundedPrefixBuffer(t *testing.T) {
	var parser Parser
	input := "<x" + strings.Repeat("界", 100000)
	segments := parser.Push(input)
	normal, _, _, _ := flatten(segments)
	if normal != input {
		t.Fatalf("emitted %d bytes, want %d", len(normal), len(input))
	}
	if len(parser.pending) > len(CloseTag)+1 {
		t.Fatalf("pending grew to %d bytes", len(parser.pending))
	}
}

func TestParserInterruptDoesNotCompletePlan(t *testing.T) {
	var parser Parser
	segments := parser.Push("<proposed_plan>\npartial")
	segments = append(segments, parser.Interrupt()...)
	_, proposed, starts, ends := flatten(segments)
	if proposed != "partial" || starts != 1 || ends != 0 {
		t.Fatalf("plan=%q starts=%d ends=%d", proposed, starts, ends)
	}
}
