package subagent

import (
	"fmt"
	"math"
	"time"
)

// ParseWaitTimeoutMS validates an untrusted millisecond count before converting
// it to time.Duration. Zero preserves the manager's configured default.
func ParseWaitTimeoutMS(ms int) (time.Duration, error) {
	if ms < 0 {
		return 0, fmt.Errorf("subagent wait timeout_ms cannot be negative")
	}
	if int64(ms) > math.MaxInt64/int64(time.Millisecond) {
		return 0, fmt.Errorf("subagent wait timeout_ms is too large")
	}
	return time.Duration(ms) * time.Millisecond, nil
}
