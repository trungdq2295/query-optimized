# query-optimizer (`qopt`)

A **connect → diagnose → verify** SQL optimizer. The AI *proposes* an
optimization (rewrite / index / partition / shard); a deterministic Go engine
**proves** a rewrite is both faster *and* behavior-preserving against a live
database before any advice is trusted. **"Generate fuzzy, verify hard."**

```
backend/    Go: the verify engine + HTTP/CLI servers
frontend/   React + Vite web UI
examples/   demo schema + slow queries (5k customers, 300k orders)
```

---

## Pick how you want to run it

There are three ways to use this, by who you are:

| You are… | You have… | Use | Needs |
|----------|-----------|-----|-------|
| **1. Engineer** | a terminal + Claude Code / Cursor | the **CLI** on this repo | Go |
| **2. Analyst** | an AI agent installed (claude/cursor), but don't live in a terminal | **`./run-local.sh`** → opens the app | Go, Node |
| **3. Anyone** | nothing — just a browser | a **hosted deploy** someone runs for you | Docker + an API key (the deployer's) |

---

### Case 1 — Engineer, on the CLI

You drive the optimizer from your editor/agent. The AI reads `SKILL.md` to learn
the rules, proposes a change, and the `qopt` binary verifies it against your DB.

```bash
cd backend
go build -o qopt ./cmd/qopt

# diagnose (cheap; -analyze runs it for real timing)
./qopt explain -engine mysql -dsn "$QOPT_DSN" -sql "SELECT ..."

# verify a proposal (JSON on stdin -> a Verdict)
echo '{"kind":"rewrite","original_sql":"...","rewritten_sql":"...","self_serve":true}' \
  | ./qopt verify -engine mysql -dsn "$QOPT_DSN"
```

The connection string (`-dsn` / `QOPT_DSN`) is a **secret** — never logged or
committed. Use a **read-only** account; the guard refuses non-SELECT regardless.

### Case 2 — Analyst, one file to open the app

No terminal knowledge needed. The launcher builds everything, loads a demo
database, and opens the web UI. It runs in **local mode**: it drives the AI
agent already installed on your machine (claude / cursor), so **no API key**.

```bash
./run-local.sh
```

On macOS you can **double-click `run-local.command`** in Finder instead.
Then use the page at <http://localhost:8080>. Re-running is fast (it skips
already-built steps). Prereqs: [Go](https://go.dev/dl/),
[Node.js](https://nodejs.org/), and an agent CLI (`claude` or `cursor-agent`).

### Case 3 — Anyone, a hosted web app

A deployer runs one container; visitors just open a browser. It runs in
**hosted mode**: the *server* holds the API key and points at one fixed demo
database, so the public side needs no AI tools and no credentials.

```bash
cp .env.example .env        # then put your QOPT_API_KEY in it
QOPT_API_KEY=sk-ant-... docker compose up --build
# open http://localhost:8080
```

`docker compose` brings up MySQL, seeds the 300k-row demo, and starts the
server. For a single SQLite-backed container instead:

```bash
docker build -t query-optimizer .
docker run -p 8080:8080 -e QOPT_MODE=hosted -e QOPT_API_KEY=sk-ant-... query-optimizer
```

---

## Configuration (env)

| Var | Default | Meaning |
|-----|---------|---------|
| `QOPT_MODE` | `local` | `local` (drive user's CLI) · `hosted` (server API key) · `verify` |
| `QOPT_ENGINE` | `sqlite` | `sqlite` · `mysql` |
| `QOPT_DSN` | — | connection string (sqlite: a file path). **Secret.** Required for the server. |
| `QOPT_API_KEY` | — | Anthropic key for hosted mode (server-side). |
| `QOPT_API_MODEL` | `claude-sonnet-4-6` | model for hosted mode |
| `QOPT_ADDR` | `:8080` | listen address |
| `QOPT_STATIC_DIR` | — | serve the built frontend from here (set by launcher/Docker) |
| `QOPT_CORS_ORIGIN` | `*` | CORS allow-origin |
| `QOPT_TIMEOUT` | `60` | per-query timeout (seconds) |

Copy `.env.example` → `.env` (gitignored) for hosted mode.

## Verdict statuses
- `verified` — faster **and** same results. Safe to present.
- `behavior_changed` — different rows. Rewrite rejected.
- `not_faster` — same rows, no speedup.
- `unverifiable` — can't prove without DDL (index/partition/shard) or without a DB.

For index/partition/shard advice the tool **never runs DDL**: apply it in a
sandbox, then use the page's *"Verify after applied"* (or `/recheck`) to measure
the before/after against the captured baseline.

## Architecture

Clean architecture — dependencies point inward. Inner layers use only the
standard library; concrete DB drivers are blank-imported in `cmd/` only.

```
backend/internal/domain                entities (Proposal, Verdict, OptKind, Progress); import nothing
backend/internal/port                  seams: SafetyValidator, QueryRunner, Verifier, Proposer, VerificationService
backend/internal/service/safety        AssertSafeSelect — SELECT-only security boundary
backend/internal/service/verification  per-kind verifier strategies + dispatcher (the growth axis)
backend/internal/service/propose       prompt build + parse; cli/ (user's CLI) and api/ (server key) proposers
backend/internal/repo/database         QueryRunner over database/sql + per-engine adapters
backend/internal/usecase/optimize      orchestration: Optimize, Recheck, Explain (no SQL, no rules)
backend/internal/handler/{cli,http}    delivery adapters
backend/internal/deps                  composition root: builds concretes, injects into ports
backend/cmd/{qopt,qopt-server,qopt-seed}   mains — the ONLY place DB drivers are imported
```

> Deeper reading: `ARCHITECTURE.md` (shape) → `DECISIONS.md` (why) →
> `EXTENDING.md` (add safely) → `STATE.md` (done vs stub).

## Relationship to the skill
`SKILL.md` holds the **rules** the AI reads to propose optimizations; this
codebase is the deterministic **verify** half.
