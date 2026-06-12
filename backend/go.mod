module github.com/trudoan/query-optimizer

go 1.25

// Single folder, package main. The "seams" are interfaces (engine.Adapter,
// Optimizer) + the typed contracts (Proposal, Verdict) — NOT subpackages.
//
// contracts.go / safety.go / engine.go / optimizer.go / runner.go use ONLY the
// standard library, so `go test` runs with zero network. Database drivers are
// imported ONLY by main.go (the CLI frontend) and the sqlite test — they are
// the one place that needs `go mod tidy` on first build. Keeping drivers out of
// the logic is deliberate. See DECISIONS.md (ADR-0005).
require modernc.org/sqlite v1.30.0

require github.com/go-sql-driver/mysql v1.10.0

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.19.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.50.9 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
