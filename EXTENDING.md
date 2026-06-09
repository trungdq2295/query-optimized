# Extending the tool

The clean-architecture layering exists so you can extend **without breaking
what works**. Each extension touches one package + the composition root
(`internal/deps`). The dependency rule guarantees a change in an outer layer
cannot ripple inward.

## The seam guarantee

| You want to add… | You write… | Inner layers edited? | Tests still pass? |
|---|---|---|---|
| a new engine (e.g. Oracle) | an adapter in `repo/database/engine.go` + driver import in `cmd/qopt` + row in `deps` | no | yes |
| a new optimization kind (e.g. `clustering`) | a `Verifier` in `service/verification` + register in `verification.New` | no | yes |
| a looser verdict (`<1%` tolerance) | a new `Verifier` (or swap the rewrite one) | no | yes |
| a new frontend (Slack / web) | a sibling of `handler/cli` + a `deps` entry | no | yes |

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

2. **Driver** — `cmd/qopt/main.go`: `import _ "github.com/godror/godror"`.

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

## Add a new frontend (Slack / web / autonomous agent)

`handler/cli` is one delivery adapter. A new one:
- builds a `domain.Proposal` (from a Slack message / HTTP body / LLM call),
- gets a `*optimize.UseCase` from `deps.BuildUseCase`,
- calls `uc.Verify(...)` / `uc.Explain(...)`, renders the `domain.Verdict`.

It reuses every inner layer unchanged. **If the frontend is autonomous** (no
human, no agent-in-the-loop), that adapter is where the `generate → verify →
retry ≤3` loop goes — feed a failed `Verdict` back to the proposer for a new
`Proposal`. Not in the core (ADR-0008).

---

## Testing an extension

- Service / usecase logic: inject a fake `port.QueryRunner` (see
  `service/verification/verification_test.go` and
  `usecase/optimize/optimize_test.go`). No DB.
- Full wiring: add a case to `internal/deps/deps_integration_test.go` — it runs
  end-to-end on pure-Go SQLite, offline.

---

## Things you must NOT change to extend

- **`service/safety`** — the security boundary. A new engine does not need it
  relaxed (ADR-0003).
- **`internal/domain` / `internal/port` shapes** — changing an entity or
  interface breaks every layer at once. Add, don't mutate.
- **The "never run DDL" rule** (ADR-0009) — index/partition stay advice-only.
