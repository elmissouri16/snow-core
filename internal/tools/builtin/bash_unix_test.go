//go:build darwin || linux

package builtin

import (
	"fmt"
	"time"
)

func testOutputCapCommand() string            { return "head -c 10000 /dev/zero | tr '\\0' 'x'" }
func testSleepCommand(d time.Duration) string { return fmt.Sprintf("sleep %.3f", d.Seconds()) }
func testListCommand(path string) string      { return "ls " + path }
func testPrintCommand(value string) string    { return "printf " + value }
func testDescendantCommand(path string) string {
	return fmt.Sprintf("(sleep 0.8; printf escaped > %s) & sleep 10", path)
}
