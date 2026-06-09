// Package optimize is the use-case (application) layer. It orchestrates the
// flow — diagnose with EXPLAIN, verify a Proposal — by calling ports. It holds
// no SQL dialect knowledge and no verification rules; those live in repo and
// service. Depends only on domain + port.
package optimize

import (
	"context"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/port"
)

// UseCase orchestrates query optimization.
type UseCase struct {
	runner       port.QueryRunner
	verification port.VerificationService
}

// New injects the data-access and verification ports.
func New(runner port.QueryRunner, verification port.VerificationService) *UseCase {
	return &UseCase{runner: runner, verification: verification}
}

// Explain diagnoses a query: returns the engine's plan as text. Cheap when
// analyze=false (plan only); analyze=true executes the query for real timing.
func (u *UseCase) Explain(ctx context.Context, engine, sql string, analyze bool, timeoutS int) (string, error) {
	return u.runner.Explain(ctx, engine, sql, analyze, timeoutS)
}

// Verify proves (or refuses) a Proposal against the live database, dispatching
// by kind through the verification service.
func (u *UseCase) Verify(ctx context.Context, p domain.Proposal, engine string, timeoutS int) (domain.Verdict, error) {
	return u.verification.Verify(ctx, p, engine, timeoutS)
}
