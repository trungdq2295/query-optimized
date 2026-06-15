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

## ADR-0008 — No code retry loop yet — **SUPERSEDED by ADR-0011**

**Decision (original):** there is no `generate → verify → retry` loop in the binary.

**Why:** `v2-migration` needed one because it drove a *headless* `cursor-agent`
subprocess that couldn't self-correct. Here the AI is the agent in the loop
(reads the `Verdict`, fixes the Proposal, re-runs). The retry loop only becomes
necessary when an **unattended** frontend ships (Slack bot, embedded agent) —
build it then, in that adapter, not in the core. Writing it now would be dead
code that couples the core to a frontend that doesn't exist.

**Update:** the unattended frontend shipped (the web UI, ADR-0012), which is
exactly the trigger this ADR named. The loop now lives in `usecase/optimize`
(`maxAttempts = 3`). See ADR-0011.

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

---

## ADR-0011 — Autonomous propose→verify→re-propose loop in the use case

**Decision:** `usecase/optimize` runs the full loop itself — diagnose, ask the
`Proposer`, verify, and on a *retryable* failure feed the failed `Verdict` back
and re-propose, up to `maxAttempts = 3`. Only a `rewrite` is retried.

**Why:** this supersedes ADR-0008. That ADR said "build the loop when an
unattended frontend ships" — the web UI (ADR-0012) is that frontend; a browser
user is not an agent who can read a `Verdict` and re-submit. The CLI `verify`
path is unchanged (still one-shot, JSON in/out) — the loop is additive, on the
`Optimize` entry point only.

**Consequence:** the core now depends on a `Proposer` port to close the loop.
It stays *optional* (`New(..., proposer, baselines)` accepts nil) so the CLI
`verify` path wires neither and is unaffected.

---

## ADR-0012 — HTTP adapter with SSE; `Optimize` + `Recheck` join `Explain`/`Verify`

**Decision:** add `handler/http` next to `handler/cli`, both thin adapters over
the same use case. `/optimize` streams progress as **Server-Sent Events** then a
terminal result; `/recheck` and `/explain` are plain JSON. The server holds the
engine + DSN; request bodies carry only SQL.

**Why:** the browser needs live feedback during a multi-second propose→verify
loop — SSE gives per-stage progress over one HTTP response (POST + a fetch
stream reader on the frontend, since `EventSource` is GET-only). Keeping the DSN
server-side is what makes a public hosted instance safe (ADR-0013/credentials
never cross the wire).

**Consequence:** the use case grew `Optimize` (loop, ADR-0011) and `Recheck`
(prove an applied system change vs a captured baseline) alongside the original
`Explain`/`Verify`. Both adapters call identical use-case methods.

---

## ADR-0013 — One binary, three run modes; the Proposer is a pluggable seam

**Decision:** a single `qopt-server` serves all modes via `QOPT_MODE`:
- `local` — drives the *user's own* CLI agent (`claude`/`cursor-agent`) headless
  via a `cli` proposer. No API key; piggybacks the user's existing login.
- `hosted` — an `api` proposer calls the Anthropic API with a key the **server**
  holds. The public side needs no AI tools and no credentials.
- `verify` — no proposer; CLI-style verify only.
`Proposer` is a `port` interface with `cli`/`api` implementations; `deps` picks
one from the mode.

**Why:** three personas (engineer with a CLI, analyst with an agent but no
terminal, and a no-AI browser user) need the same engine but different proposal
sources. Making the proposer a seam means a new source (e.g. a local model) is
one new implementation, not a fork.

**Consequence:** the API key is read from env in the `api` proposer only, never
logged or returned in errors. The DSN is always a server secret.

---

## ADR-0014 — The server can serve the built frontend (`QOPT_STATIC_DIR`)

**Decision:** if `QOPT_STATIC_DIR` is set, the server serves the built SPA as a
catch-all, falling back to `index.html` for client-side routes; API routes win
because they are registered as exact paths.

**Why:** a packaged local app (analyst persona) and a hosted deploy should be
*one* process, not "run a web server too." `http.ServeFile` already rejects
`..` traversal, so no separate static host is needed.

---

## ADR-0015 — Demo data loads via a Go seeder, not a `sqlite3`/`mysql` client

**Decision:** `cmd/qopt-seed` reads a `.sql` file and executes it through the
already-imported Go drivers. Seed files have no `;` inside string literals, so
it strips comment lines then splits on `;`.

**Why:** a fresh clone must run with *only* the Go toolchain — requiring a
`sqlite3` or `mysql` client binary breaks the "clone and run" promise for the
analyst and Docker personas. Keeps the demo dependency-free.

---

## ADR-0016 — Package for three personas (launcher + Docker), not one entry point

**Decision:** ship `run-local.sh` / `run-local.command` (build FE+BE, seed
SQLite, open the browser in local mode) **and** a multi-stage `Dockerfile` +
`docker-compose.yml` (MySQL + seed + server in hosted mode), on top of the raw
CLI.

**Why:** the three personas differ in what they have, not in what the engine
does. One launcher file for the non-CLI analyst; one `docker compose up` for a
deployer; the bare `go build` for the engineer. Same binary underneath.

---

## ADR-0017 — Prove correctness by end-to-end runs, not unit tests

**Decision:** new code (proposers, HTTP, baseline store, seeder) ships **without
unit tests**; correctness is shown by real runs against a real database. Existing
tests (`safety`, `verification`, `usecase`, `deps` integration) stay green but
are not expanded.

**Why:** the owner's stated preference — mocked tests can pass while the real
DB/agent path breaks; the value is in the measured before/after, which is the
product's whole premise ("verify hard"). Investing in mocks for the I/O-bound
adapters would test the mocks, not the behavior.

**Consequence:** `go test ./...` is still the offline gate, but the source of
truth for new features is a documented live run (see STATE.md proofs). Don't add
mock-heavy tests for adapters; verify them end-to-end instead.
