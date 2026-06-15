# Working notes for Claude

Durable context for this repo, so any machine/session has it (not tied to local memory).

## How the user likes to work
- **No unit tests.** Prove correctness by real end-to-end runs against a real DB, not mocks. (Verified runs: rewrite 210× on sqlite, 574× on mysql.)
- Terse communication is fine.

## Repo / git
- Remote `origin` = github.com/trungdq2295/query-optimized, default branch `master`.
- **`master` is branch-protected: no direct pushes.** All changes via a branch + PR. `enforce_admins` on (no bypass), 0 required approvals (solo dev can merge own PR).
- On this kind of machine, gh CLI may be authed to a work enterprise, not github.com — github.com pushes used a personal access token inline (not persisted).

## Project state (2026-06-15)
- Core "AI proposes, deterministic Go verifies" works. Three run modes: `local` (drive user's claude/cursor CLI, no key), `hosted` (server API key), `verify`.
- Clone-and-run set up for 3 personas: engineer CLI (`backend/ go build`), analyst (`./run-local.sh` / `.command`), hosted browser (`docker compose up`). See README.
- Hosted mode only page-load tested — never a full real-API-key proposal end-to-end. Index/recheck ran once, not hardened.

## Parked ideas (revive only on a real requirement)
Brainstormed, then parked to avoid over-building. Leading direction first:
1. **Verify-as-a-CI-gate / PR bot** — PR touching a query → verifier runs on a replica → posts "verified Nx faster, same rows" or "BLOCKED: behavior changed." Dodges the demo-DB / `unverifiable` weakness.
2. Generalize "prove the AI didn't break it" to any checkable AI change (ORM swaps, migrations); SQL = beachhead.
3. Invert to a regression *catcher* (alert on deploy-caused slowdowns).
4. Standalone SQL equivalence sandbox.

Known weak spots: verification fidelity (demo DB ≠ prod scale/skew/NULLs/collation); high-value advice (index/partition/shard) is `unverifiable` without DDL.
