# Extending the tool

The clean-architecture layering exists so you can extend **without breaking
what works**. Each extension touches one package + the composition root
(`internal/deps`). The dependency rule guarantees a change in an outer layer
cannot ripple inward.

> Paths are under `backend/` (e.g. `backend/cmd/qopt`, `backend/internal/...`).

## The seam guarantee

| You want to add… | You write… | Inner layers edited? |
|---|---|---|
| a new engine (e.g. Oracle) | an adapter in `repo/database/engine.go` + driver import in `cmd/*` + row in `deps` | no |
| a new optimization kind (e.g. `clustering`) | a `Verifier` in `service/verification` + register in `verification.New` | no |
| a looser verdict (`<1%` tolerance) | a new `Verifier` (or swap the rewrite one) | no |
| a new proposer (e.g. a local model) | a `port.Proposer` impl in `service/propose/*` + a `deps.BuildProposer` case | no |
| a new run mode | a `Mode` const + a `deps.BuildProposer` branch | no |
| a new frontend (Slack / bot) | a sibling of `handler/{cli,http}` + a `deps` entry | no |

If an "extension" forces you to edit `domain` or `port`, stop — you're probably
breaking a seam. Adding a constant to `domain` is fine; changing an interface or
an entity shape touches every implementer at once.

---

## Add an engine (e.g. Oracle)

1. **Adapter** — `internal/repo/database/engine.go`:
   ```go
   type oracleAdapter struct{}
   func (oracleAdapter) name() string { return "oracle" }
   func (oracleAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error { return nil }
   func (oracleAdapter) explainSQL(body string, analyze bool) string { return "EXPLAIN PLAN FOR " + body }
   func (oracleAdapter) supportsManualIndex() bool       { return true }
   func (oracleAdapter) supportsHypotheticalIndex() bool { return false }
   ```
   Register in `init()`: `register(oracleAdapter{})`.

2. **Driver** — blank-import in each entry that needs it (`cmd/qopt`,
   `cmd/qopt-server`, `cmd/qopt-seed`): `import _ "github.com/godror/godror"`.

3. **Wiring** — `internal/deps/deps.go`, add to `engineToDriver`:
   `"oracle": "godror",`

No inner layer changes. The repo talks to Oracle through the private `adapter`
interface; the usecase only ever sees an engine name.

---

## Add an optimization kind (e.g. `clustering`)

1. **Constant** — `internal/domain/proposal.go`:
   ```go
   const KindClustering OptKind = "clustering"
   ```

2. **Verifier** — `internal/service/verification/` (new file or in `advisory.go`).
   Clustering is physical layout → `unverifiable`:
   ```go
   type ClusteringVerifier struct{}
   func NewClusteringVerifier() *ClusteringVerifier { return &ClusteringVerifier{} }
   func (*ClusteringVerifier) Kind() domain.OptKind { return domain.KindClustering }
   func (*ClusteringVerifier) Verify(_ context.Context, _ domain.Proposal, _ string, _ int) (domain.Verdict, error) {
       return domain.Verdict{Status: domain.StatusUnverifiable, Notes: []string{
           "Clustering changes physical layout — propose only; confirm via query_history after the DBA applies it.",
       }}, nil
   }
   ```

3. **Register** — in `verification.New`:
   ```go
   s.register(NewClusteringVerifier())
   ```

`usecase.Verify` now dispatches `kind:"clustering"` automatically — the
dispatcher is keyed by `Kind()`.

---

## Add a tolerant rewrite verdict (`<1%` band)

Don't touch the existing `RewriteVerifier`. Either register a tolerant verifier
in place of it, or add a new kind the AI can choose. The comparison lives
entirely inside `Verify`; widen it there (see ADR-0007):
```go
diff := math.Abs(float64(old.RowCount-neu.RowCount)) / float64(old.RowCount)
behavior := diff < 0.01
```
`domain`/`port`/`usecase` never learn about tolerance — the seam holds.

---

## Add a proposer (e.g. a local model, a different API)

The AI half is a seam — `port.Proposer` (`Name()` + `Propose(ctx, ProposeInput)`).
`service/propose` already ships `cli` (drives the user's claude/cursor) and
`api` (server-held key). To add a third:

1. **Impl** — new package under `service/propose/`. Reuse the shared
   `propose.BuildPrompt` / `propose.ParseProposal` so the prompt + parsing match
   the others; only the transport differs.
2. **Wire** — add a branch in `deps.BuildProposer(mode)` returning your impl.

The use-case loop (ADR-0011) and both handlers call `Proposer` through the
interface — they never learn which one is wired.

## Add a run mode

Modes select a proposer. Add one:

1. **Const** — a `Mode` value in `deps` (alongside `ModeLocal`/`ModeHosted`/`ModeVerify`).
2. **Branch** — handle it in `deps.BuildProposer(mode)` (pick/skip a proposer).

`cmd/qopt-server` passes `QOPT_MODE` straight through; nothing else changes.

## Add a new frontend (Slack / bot / autonomous agent)

`handler/cli` and `handler/http` are delivery adapters. A new one:
- gets a `*optimize.UseCase` from `deps.BuildUseCase`,
- calls `uc.Optimize(...)` (propose→verify→retry, with a progress callback) or
  the lower-level `uc.Verify(...)` / `uc.Explain(...)` / `uc.Recheck(...)`,
- renders the `domain.OptimizeResult` / `domain.Verdict`.

It reuses every inner layer unchanged. The `generate → verify → retry ≤3` loop
already lives in the core (`usecase.Optimize`, ADR-0011) — a new autonomous
frontend just calls it; it does **not** re-implement the loop.

---

## Testing an extension

Correctness is shown by **real end-to-end runs**, not mock-heavy unit tests
(ADR-0017). Verify a new feature against a real DB (and a real agent/API for a
proposer), and record the run.

- The existing offline suite must still pass: `service/verification` and
  `usecase/optimize` inject a fake `port.QueryRunner` (no DB); `deps`
  integration runs the full wiring on pure-Go SQLite. Run `go test ./...`.
- Don't add mock-heavy tests for I/O adapters (proposers, HTTP, baseline) —
  prove them with a documented live run instead.

---

## Things you must NOT change to extend

- **`service/safety`** — the security boundary. A new engine does not need it
  relaxed (ADR-0003).
- **`internal/domain` / `internal/port` shapes** — changing an entity or
  interface breaks every layer at once. Add, don't mutate.
- **The "never run DDL" rule** (ADR-0009) — index/partition stay advice-only.
