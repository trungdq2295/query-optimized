package verification

import (
	"context"
	"fmt"
	"math"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/port"
)

// RewriteVerifier proves a rewrite by RUNNING old vs new and comparing row
// count + sample fingerprint (behavior) and timing (speed). This is the only
// provable kind.
type RewriteVerifier struct {
	runner port.QueryRunner
}

// NewRewriteVerifier injects the QueryRunner used to execute both queries.
func NewRewriteVerifier(runner port.QueryRunner) *RewriteVerifier {
	return &RewriteVerifier{runner: runner}
}

func (*RewriteVerifier) Kind() domain.OptKind { return domain.KindRewrite }

func (v *RewriteVerifier) Verify(ctx context.Context, p domain.Proposal, engine string, timeoutS int) (domain.Verdict, error) {
	if p.RewrittenSQL == "" {
		return domain.Verdict{}, fmt.Errorf("%w: rewrite has empty rewritten_sql", domain.ErrInvalidProposal)
	}

	// WARNING: runs BOTH queries in full. The original is the slow one — the
	// timeout is the only brake.
	old, err := v.runner.Run(ctx, engine, p.OriginalSQL, timeoutS)
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("running original: %w", err)
	}
	neu, err := v.runner.Run(ctx, engine, p.RewrittenSQL, timeoutS)
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("running rewrite: %w", err)
	}

	var notes []string
	rowsMatch := old.RowCount == neu.RowCount
	sampleMatch := old.SampleHash == neu.SampleHash
	behavior := rowsMatch && sampleMatch

	switch {
	case !rowsMatch:
		notes = append(notes, fmt.Sprintf(
			"Row count differs: old=%d new=%d — rewrite is NOT behavior-preserving.",
			old.RowCount, neu.RowCount))
	case !sampleMatch:
		notes = append(notes, "Row counts match but sampled content differs — "+
			"check ORDER BY / column selection before trusting the rewrite.")
	}

	var speedup float64
	if neu.ElapsedS > 0 {
		speedup = math.Round((old.ElapsedS/neu.ElapsedS)*10000) / 10000
	}

	status := domain.StatusVerified
	switch {
	case !behavior:
		status = domain.StatusBehaviorChanged
	case speedup <= 1.0:
		status = domain.StatusNotFaster
		notes = append(notes, fmt.Sprintf(
			"Rewrite is behavior-preserving but not faster (%.4fs -> %.4fs).",
			old.ElapsedS, neu.ElapsedS))
	}

	return domain.Verdict{
		Status:            status,
		BehaviorPreserved: domain.BoolPtr(behavior),
		Speedup:           domain.FloatPtr(speedup),
		Old:               &old,
		New:               &neu,
		Notes:             notes,
	}, nil
}
