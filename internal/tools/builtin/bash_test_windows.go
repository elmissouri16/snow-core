//go:build windows

package builtin

import (
	"fmt"
	"time"
)

func testOutputCapCommand() string { return "[Console]::Out.Write(('x' * 10000))" }
func testSleepCommand(d time.Duration) string {
	return fmt.Sprintf("Start-Sleep -Milliseconds %d", d.Milliseconds())
}
func testListCommand(path string) string {
	return "Get-Item -LiteralPath '" + path + "' | Select-Object -ExpandProperty Name"
}
func testPrintCommand(value string) string { return "[Console]::Out.Write('" + value + "')" }
func testDescendantCommand(path string) string {
	return fmt.Sprintf(`Start-Process powershell.exe -ArgumentList '-NoLogo','-NoProfile','-NonInteractive','-Command','Start-Sleep -Milliseconds 800; Set-Content -LiteralPath ''%s'' -Value escaped'; Start-Sleep -Seconds 10`, path)
}
