package compact

import (
	"fmt"
	"strings"
	"testing"
)

var referenceCheckpointSink string

func BenchmarkNormalizeCheckpoint(b *testing.B) {
	for _, shape := range []string{"concentrated", "distributed"} {
		for _, n := range []int{100, 400, 1600} {
			var text strings.Builder
			text.WriteString(WorkingStateTitle + "\n")
			for i := range n {
				if shape == "concentrated" && i == 0 {
					text.WriteString("\n## Current working state\n")
				}
				if shape == "distributed" && i%((n+11)/12) == 0 {
					text.WriteString("\n## " + WorkingStateSections[i/((n+11)/12)] + "\n")
				}
				fmt.Fprintf(&text, "- File %04d: retain implementation decisions and verification details.\n", i)
			}
			summary := text.String()
			b.Run(fmt.Sprintf("%s/lines%d_bytes%d", shape, n, len(summary)), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					result, _, err := NormalizeWorkingStateCheckpoint(b.Context(), summary, nil)
					if err != nil {
						b.Fatal(err)
					}
					referenceCheckpointSink = result
				}
			})
		}
	}
}

func referenceLegacyCanonical(summary string) string {
	known := make(map[string]bool, len(WorkingStateSections))
	for _, section := range WorkingStateSections {
		known[section] = true
	}
	type chunk struct {
		heading string
		body    string
	}
	var preamble []string
	var chunks []chunk
	var current *chunk
	flush := func() {
		if current != nil {
			chunks = append(chunks, *current)
			current = nil
		}
	}
	for line := range strings.SplitSeq(summary, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = &chunk{heading: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if current == nil {
			if strings.TrimSpace(line) != WorkingStateTitle {
				preamble = append(preamble, line)
			}
			continue
		}
		if current.body != "" {
			current.body += "\n"
		}
		current.body += line
	}
	flush()

	sections := make(map[string]string, len(WorkingStateSections))
	var extras []chunk
	for _, item := range chunks {
		body := strings.TrimSpace(item.body)
		if !known[item.heading] {
			extras = append(extras, chunk{heading: item.heading, body: body})
			continue
		}
		prior, exists := sections[item.heading]
		switch {
		case !exists:
			sections[item.heading] = body
		case checkpointBodyEmpty(prior) && !checkpointBodyEmpty(body):
			sections[item.heading] = body
		case !checkpointBodyEmpty(body) && !strings.Contains(prior, body):
			sections[item.heading] = strings.TrimSpace(prior) + "\n" + body
		}
	}

	var out strings.Builder
	out.WriteString(WorkingStateTitle)
	if text := strings.TrimSpace(strings.Join(preamble, "\n")); text != "" {
		out.WriteString("\n\n")
		out.WriteString(text)
	}
	for _, item := range extras {
		if item.heading == "" {
			continue
		}
		out.WriteString("\n\n## ")
		out.WriteString(item.heading)
		if item.body != "" {
			out.WriteByte('\n')
			out.WriteString(item.body)
		}
	}
	for _, section := range WorkingStateSections {
		body := strings.TrimSpace(sections[section])
		if body == "" {
			body = "- None recorded."
		}
		out.WriteString("\n\n## ")
		out.WriteString(section)
		out.WriteByte('\n')
		out.WriteString(body)
	}
	return out.String()
}

func TestCanonicalCheckpointEquivalence(t *testing.T) {
	cases := []string{"", "\n", "before\n\n## Current working state\n\nline\n\nline\n", "## Extension\ntext\n## Extension\nnext", "## Current working state\n- None recorded.\n## Current working state\nactual"}
	for n := range 1000 {
		var input strings.Builder
		for j := range 1 + n%60 {
			switch (n*7 + j*13) % 7 {
			case 0:
				input.WriteString("\n## " + WorkingStateSections[(n+j)%len(WorkingStateSections)] + "\n")
			case 1:
				input.WriteString("\n\n")
			case 2:
				input.WriteString("- None recorded.\n")
			case 3:
				input.WriteString("## Extra section\n")
			case 4:
				input.WriteString("  unicode é中 content \n")
			case 5:
				input.WriteString(WorkingStateTitle + "\n")
			default:
				fmt.Fprintf(&input, "- Item %d repeated %d\n", n, j)
			}
		}
		cases = append(cases, input.String())
	}
	for i, input := range cases {
		if got, want := canonicalizeWorkingStateCheckpoint(input), referenceLegacyCanonical(input); got != want {
			t.Fatalf("case %d output differs", i)
		}
	}
}
