# Decisions (ADRs)

Short records of *why* the tool is built this way. Read these before changing
anything load-bearing — each decision has a reason that may bite you if reversed
blindly.

---

## ADR-0001 — Split into transform (AI) + verify (code)

**Decision:** the AI proposes optimizations; deterministic Go verifies them.

**Why:** an LLM is good at proposing a rewrite (reads EXPLAIN + rules) but must
not be trusted to be *correct*. A faster query that returns different rows is a
bug. So a deterministic layer runs old vs new and compares before any advice is
shown. Same "generate fuzzy, verify hard" pattern proven in `v2-migration-tool`.

**Consequence:** the two layers must stay cleanly separable. The contracts
(`Proposal`/`Verdict`) are the only thing crossing the boundary.

---

## ADR-0002 — Go, not Python (the verify layer)

**Decision:** write the verify engine in Go.

**Why:** the *owner of this tool reads Go better than Python* and Go best of
all the realistic options (Java judged too heavy for a small CLI). The verify
layer is the truth-decider — if you can't audit the layer that says
"behavior_changed", you can't trust it. Audit-ability beat Python's slightly
nicer DB ecosystem. Bonus: Go ships a single static binary — no portable-Python
bootstrap like the `v2-migration` `.exe` needed. Verify is I/O-bound (waiting on
the DB), so Go's speed is not the reason; audit-ability + distribution are.

**Trade-off accepted:** Go's mssql/snowflake drivers are thinner than Python's.
Acceptable — fancy plan parsing is the AI's job, not the driver's.

---

## ADR-0003 — Port the security guard line-for-line, don't reinvent

**Decision:** `AssertSafeSelect` is a faithful port of the Python
`assert_safe_select` (same forbidden-keyword set, same single-statement rule).

**Why:** it is the boundary between user input and the database. The Python
version is proven. Reinventing it risks opening a hole (a "verify" that runs
`DROP TABLE`). Known limitation kept on purpose: it is a keyword **denylist**,
so it can over-reject (fail safe) — that's the correct direction to err.

**Consequence:** every query path (`timedRun`, `explainPlan`) calls it. Do not
add a path that skips it.

---

## ADR-0004 — Optimization KIND is the primary extension axis

**Decision:** `Optimizer` is an interface keyed by `OptKind`; each kind declares
how it is verified.

**Why:** unlike `v2-migration` (one transform), this tool has many optimization
types and they differ in how far self-service goes (rewrite = provable, index =
propose-only, shard = escalate). Modelling each as a plug means adding
`partition`/`clustering` later touches one new file, not the core.

---

## ADR-0005 — DB drivers live ONLY in `cmd/qopt`

**Decision:** the inner layers (`domain`, `port`, `service`, `usecase`,
`repo/database`) use the stdlib `database/sql` API only. Concrete drivers
(`go-sql-driver/mysql`, `modernc.org/sqlite`) are blank-imported in
`cmd/qopt/main.go`. The engine→driver map lives in `internal/deps`.

**Why:** keeps the logic build-able and testable with zero network (`go test`
runs offline against pure-Go SQLite). Drivers are a composition concern.
Adding an engine driver = one import in `cmd/qopt` + one row in `deps`.

---

## ADR-0006 — Proposal in via JSON on stdin

**Decision:** `qopt verify` reads a JSON `Proposal` from stdin, writes JSON
`Verdict` to stdout.

**Why:** language-agnostic and frontend-neutral. The AI (any language, any host)
just writes JSON and pipes it. A Slack bot or web UI reuses the exact same
contract. Flags would couple the interface to the CLI.

---

## ADR-0007 — Behavior match is EXACT now; tolerance is pluggable later

**Decision:** a rewrite is behavior-preserving only if row count **and** sample
hash match exactly. No `<1%` tolerance band yet.

**Why:** exact is the safe default and the simplest to trust. `v2-migration`
needed a tolerance band because migration legitimately shifts values; query
optimization should return *identical* data. If a tolerant verdict is ever
needed, it belongs **inside the relevant `Optimizer`** (e.g. a
`rewriteTolerantOptimizer`), not bolted onto the core — that keeps the seam.

---

## ADR-0008 — No code retry loop yet

**Decision:** there is no `generate → verify → retry` loop in the binary.

**Why:** `v2-migration` needed one because it drove a *headless* `cursor-agent`
subprocess that couldn't self-correct. Here the AI is the agent in the loop
(reads the `Verdict`, fixes the Proposal, re-runs). The retry loop only becomes
necessary when an **unattended** frontend ships (Slack bot, embedded agent) —
build it then, in that adapter, not in the core. Writing it now would be dead
code that couples the core to a frontend that doesn't exist.

---

## ADR-0009 — The tool NEVER runs DDL

**Decision:** index/partition advice is text output; `Verify` returns
`unverifiable` for those kinds. No write path exists in the binary.

**Why:** safety + scope. Index = the main self-serve *advice* win, but applying
it is the DBA's call. The live demo proves the prediction by applying the index
**outside** the tool, in a throwaway sandbox — the tool itself stays SELECT-only.
On Postgres, HypoPG is the path to confirm an index *without* DDL (noted in the
verdict); wire it if Postgres index-verify becomes a real requirement.

---

## ADR-0010 — Clean architecture (layers), not a flat package

**Decision:** structure as `domain → port → service/repo → usecase → handler →
deps → cmd`, mirroring the `warden` service. The first build was a single flat
`package main`; this replaced it.

**Why:** the owner asked for it explicitly, to match the team's `warden`
convention so extension is safe and familiar. Concrete payoffs:
- the **dependency rule** (everything points inward to `domain`) means a change
  to the CLI, an engine driver, or a verifier cannot ripple into the core;
- ports make every layer **unit-testable with fakes** — `usecase` and
  `verification` tests run with no database at all;
- the **composition root** (`deps`) is the single place wiring lives, so
  swapping an implementation is a one-line change.

**Consequence:** more files than a flat layout, but each has one job and the
seams are physical (package boundaries), not just conventional. The interfaces
that were informal in the flat version are now `internal/port`.
