package subagent

import (
	"math"
	"strconv"
	"testing"
	"time"
)

func TestParseWaitTimeoutMS(t *testing.T) {
	for _, test := range []struct {
		name string
		ms   int
		want time.Duration
		err  bool
	}{
		{name: "negative", ms: -1, err: true},
		{name: "default", ms: 0, want: 0},
		{name: "normal", ms: 250, want: 250 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseWaitTimeoutMS(test.ms)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("ParseWaitTimeoutMS(%d) = %s, %v", test.ms, got, err)
			}
		})
	}
	if strconv.IntSize == 64 {
		tooLarge := int(math.MaxInt64/int64(time.Millisecond) + 1)
		if _, err := ParseWaitTimeoutMS(tooLarge); err == nil {
			t.Fatal("overflowing millisecond count was accepted")
		}
	}
}
