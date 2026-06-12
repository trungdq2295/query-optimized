// Package port declares the interfaces (ports) that cross layer boundaries.
// It imports domain only. Outer layers (repo, service, usecase, handler) depend
// on these abstractions, never on each other's concretions — that is what keeps
// the architecture clean and the layers swappable.
package port

import (
	"context"

	"github.com/trudoan/query-optimizer/internal/domain"
)

type (
	// SafetyValidator is THE security boundary. It decides whether a SQL string
	// is a single read-only SELECT/WITH that may reach the database.
	// Implemented by service/safety. Injected into the repo so EVERY DB access
	// is validated regardless of caller.
	SafetyValidator interface {
		AssertSafeSelect(sql string) (string, error)
	}

	// QueryRunner is the data-access port: it talks to a real database. The
	// engine dialect (EXPLAIN syntax, session guards) is hidden behind it — the
	// caller only passes an engine name. Implemented by repo/database.
	QueryRunner interface {
		// Run executes a SELECT and returns its fingerprint (timing + row count
		// + sample hash). Validates via SafetyValidator first.
		Run(ctx context.Context, engine, sql string, timeoutS int) (domain.RunResult, error)
		// Explain returns the query plan as text. analyze=true executes the query.
		Explain(ctx context.Context, engine, sql string, analyze bool, timeoutS int) (string, error)
		// EngineExists reports whether an adapter is registered for engine.
		EngineExists(engine string) bool
		// SupportsManualIndex reports whether the engine has user B-tree indexes
		// (false on Snowflake/BigQuery). Lets index advice be phrased honestly.
		SupportsManualIndex(engine string) bool
		// SupportsHypotheticalIndex reports whether index advice can be confirmed
		// WITHOUT DDL (Postgres + HypoPG).
		SupportsHypotheticalIndex(engine string) bool
	}

	// Verifier proves (or refuses) one OptKind. Each kind is a strategy:
	// rewrite runs old vs new; index/partition/shard are advisory. Verifiers
	// depend on QueryRunner when they need to execute. Implemented by
	// service/verification.
	Verifier interface {
		Kind() domain.OptKind
		Verify(ctx context.Context, p domain.Proposal, engine string, timeoutS int) (domain.Verdict, error)
	}

	// VerificationService dispatches a Proposal to the Verifier matching its
	// Kind. This is what the usecase calls — it never knows the per-kind rules.
	VerificationService interface {
		Verify(ctx context.Context, p domain.Proposal, engine string, timeoutS int) (domain.Verdict, error)
	}

	// Proposer is the AI half: it reads the slow query + its EXPLAIN plan and
	// produces a Proposal (a rewrite, or an index/partition/shard recommendation).
	// It is deliberately an interface so the proposer is pluggable — a local
	// agent CLI (claude/cursor), a hosted LLM API call, or a fake for tests.
	// This is the seam that lets one backend serve local + hosted + bot.
	Proposer interface {
		// Name identifies the proposer in results (e.g. "cli:claude", "api").
		Name() string
		// Propose returns one Proposal for the given input. On a retry, the input
		// carries the prior failed attempt + verdict so the proposer can correct.
		Propose(ctx context.Context, in domain.ProposeInput) (domain.Proposal, error)
	}

	// BaselineStore persists "before" snapshots so a system change (index/
	// partition) the tool cannot apply can still be PROVEN after an engineer
	// applies it. Implemented in-memory for a single process; swap for a durable
	// store by changing one line in deps.
	BaselineStore interface {
		Save(b domain.Baseline) (string, error) // returns the assigned ID
		Get(id string) (domain.Baseline, bool)
	}
)
