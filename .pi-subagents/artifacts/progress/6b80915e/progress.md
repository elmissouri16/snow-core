# Progress

## Status
Complete

## Tasks
- [x] internal/auth/filestore.go: FileStore (atomic write, 0600, corrupt-file safety), ResolveAPIKey helper, ErrNoCredential
- [x] internal/provider/fake/fake.go: scripted fake provider (replay per call), NewWithModels, NewRecorded + RecordedCalls
- [x] Unit tests: auth filestore (round-trip, perms, atomics, env fallback, priority, corrupt, missing), fake (replay order, args parse, call count, recorded, cancel)

## Files Changed
- internal/auth/filestore.go (new)
- internal/auth/filestore_test.go (new)
- internal/provider/fake/fake.go (new)
- internal/provider/fake/fake_test.go (new)

## Notes
- persistCredential defined type bypasses redacting MarshalJSON so secrets persist unredacted on disk (redaction stays for logging only).
- fake: ErrExhausted sentinel exported for test assertions.
- go vet clean, gofmt clean, go test ./internal/auth/... ./internal/provider/fake/... passes (14 + 10 tests), race detector clean, stdlib only.
