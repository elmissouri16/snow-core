$ErrorActionPreference = "Stop"

function Assert-NativeSuccess([string]$Step) {
  if ($LASTEXITCODE -ne 0) { throw "$Step failed with exit code $LASTEXITCODE" }
}

Write-Host "Checking formatting"
$goFiles = (Get-ChildItem cmd,internal,pkg -Recurse -Filter *.go).FullName
$unformatted = gofmt -l $goFiles
Assert-NativeSuccess "gofmt"
if ($unformatted) { throw "gofmt required:`n$unformatted" }

Write-Host "Running focused Windows path/write/bash tests"
go test ./internal/tools/builtin -run 'Windows|PathGuard|Write|Bash' -count=1
Assert-NativeSuccess "focused tests"

Write-Host "Running full test suite"
go test ./... -count=1
Assert-NativeSuccess "full tests"

Write-Host "Running vet"
go vet ./...
Assert-NativeSuccess "go vet"

Write-Host "Building production binary"
go build -o snow-windows-test.exe ./cmd/snow
Assert-NativeSuccess "production build"
Remove-Item -Force snow-windows-test.exe

Write-Host "Windows verification complete"
