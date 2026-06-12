// Package optimize is the use-case (application) layer. It orchestrates the
// flow — diagnose with EXPLAIN, propose with the AI, verify the proposal,
// recheck after a system change — by calling ports. It holds no SQL dialect
// knowledge and no verification rules; those live in repo and service. Depends
// only on domain + port.
package optimize

import (
	"context"
	"fmt"
	"math"

	"github.com/trudoan/query-optimizer/internal/domain"
	"github.com/trudoan/query-optimizer/internal/port"
)

// maxAttempts caps the propose -> verify -> re-propose loop for rewrites. The
// proposer (agent or LLM) gets the failed verdict back and tries again. Cap
// keeps an autonomous frontend from looping forever (ADR-0008 in spirit).
const maxAttempts = 3

// UseCase orchestrates query optimization. proposer and baselines are optional:
// a pure-verify frontend (the CLI) can leave them nil and still use Explain /
// Verify. Optimize / Recheck require them.
type UseCase struct {
	runner       port.QueryRunner
	verification port.VerificationService
	proposer     port.Proposer       // optional
	baselines    port.BaselineStore  // optional
}

// New injects the data-access and verification ports. proposer/baselines may be
// nil for frontends that only diagnose + verify a hand-written proposal.
func New(runner port.QueryRunner, verification port.VerificationService, proposer port.Proposer, baselines port.BaselineStore) *UseCase {
	return &UseCase{runner: runner, verification: verification, proposer: proposer, baselines: baselines}
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

// Optimize runs the full loop: diagnose -> propose -> verify, retrying a rewrite
// (with the failed verdict fed back) until it verifies or attempts run out. For
// a non-rewrite kind (index/partition/shard) it captures a baseline so the user
// can prove the effect later, after an engineer applies the change.
//
// emit, if non-nil, receives a Progress event at each phase so a streaming
// frontend can show live steps. A non-streaming caller passes nil.
func (u *UseCase) Optimize(ctx context.Context, engine, sql string, timeoutS int, emit func(domain.Progress)) (domain.OptimizeResult, error) {
	if u.proposer == nil {
		return domain.OptimizeResult{}, fmt.Errorf("optimize requires a proposer; none wired")
	}
	report := func(p domain.Progress) {
		if emit != nil {
			emit(p)
		}
	}

	report(domain.Progress{Stage: domain.StageDiagnosing, Message: "reading EXPLAIN plan"})
	plan, err := u.runner.Explain(ctx, engine, sql, false, timeoutS)
	if err != nil {
		return domain.OptimizeResult{}, fmt.Errorf("diagnose (explain): %w", err)
	}

	in := domain.ProposeInput{Engine: engine, OriginalSQL: sql, Plan: plan}
	var p domain.Proposal
	var v domain.Verdict
	attempt := 0
	for attempt < maxAttempts {
		in.Attempt = attempt
		report(domain.Progress{Stage: domain.StageProposing, Attempt: attempt + 1, Message: "asking " + u.proposer.Name()})
		p, err = u.proposer.Propose(ctx, in)
		if err != nil {
			return domain.OptimizeResult{}, fmt.Errorf("propose (attempt %d): %w", attempt+1, err)
		}
		report(domain.Progress{Stage: domain.StageVerifying, Attempt: attempt + 1, Message: "proving against the database"})
		v, err = u.verification.Verify(ctx, p, engine, timeoutS)
		if err != nil {
			return domain.OptimizeResult{}, fmt.Errorf("verify (attempt %d): %w", attempt+1, err)
		}
		attempt++

		// Only a rewrite is retryable, and only when it failed for a reason a
		// re-proposal could fix. Anything else (verified, or a non-rewrite kind)
		// ends the loop.
		if p.Kind != domain.KindRewrite {
			break
		}
		if v.Status == domain.StatusVerified {
			break
		}
		// feed the failure back for the next attempt
		in.PriorRewrite = p.RewrittenSQL
		in.PriorVerdict = &v
	}

	res := domain.OptimizeResult{
		Proposal: p, Verdict: v, Engine: engine, Attempts: attempt, Proposer: u.proposer.Name(),
	}

	// For a system change the tool can't apply, capture a "before" so the user
	// can prove the effect after an engineer applies it.
	if p.Kind != domain.KindRewrite && u.baselines != nil {
		report(domain.Progress{Stage: domain.StageBaseline, Message: "capturing before-snapshot for verify-after-applied"})
		b, berr := u.captureBaseline(ctx, engine, sql, plan, timeoutS)
		if berr == nil {
			res.BaselineID = b.ID
		}
		// a failed baseline capture is non-fatal — the advice still stands
	}
	report(domain.Progress{Stage: domain.StageDone})
	return res, nil
}

// CaptureBaseline records a "before" snapshot (plan + timed run) for sql.
func (u *UseCase) CaptureBaseline(ctx context.Context, engine, sql string, timeoutS int) (domain.Baseline, error) {
	if u.baselines == nil {
		return domain.Baseline{}, fmt.Errorf("no baseline store wired")
	}
	plan, err := u.runner.Explain(ctx, engine, sql, false, timeoutS)
	if err != nil {
		return domain.Baseline{}, fmt.Errorf("baseline explain: %w", err)
	}
	return u.captureBaseline(ctx, engine, sql, plan, timeoutS)
}

func (u *UseCase) captureBaseline(ctx context.Context, engine, sql, plan string, timeoutS int) (domain.Baseline, error) {
	run, err := u.runner.Run(ctx, engine, sql, timeoutS)
	if err != nil {
		return domain.Baseline{}, fmt.Errorf("baseline run: %w", err)
	}
	b := domain.Baseline{Engine: engine, SQL: sql, Plan: plan, Run: run}
	id, err := u.baselines.Save(b)
	if err != nil {
		return domain.Baseline{}, err
	}
	b.ID = id
	return b, nil
}

// Recheck proves the effect of a system change AFTER an engineer applied it:
// re-run the original query, diff against the stored baseline. Everything is
// measured — nothing is predicted.
func (u *UseCase) Recheck(ctx context.Context, baselineID string, timeoutS int) (domain.RecheckResult, error) {
	if u.baselines == nil {
		return domain.RecheckResult{}, fmt.Errorf("no baseline store wired")
	}
	b, ok := u.baselines.Get(baselineID)
	if !ok {
		return domain.RecheckResult{}, fmt.Errorf("%w: baseline %q", domain.ErrNoBaseline, baselineID)
	}

	afterPlan, err := u.runner.Explain(ctx, b.Engine, b.SQL, false, timeoutS)
	if err != nil {
		return domain.RecheckResult{}, fmt.Errorf("recheck explain: %w", err)
	}
	after, err := u.runner.Run(ctx, b.Engine, b.SQL, timeoutS)
	if err != nil {
		return domain.RecheckResult{}, fmt.Errorf("recheck run: %w", err)
	}

	rowsPreserved := b.Run.RowCount == after.RowCount && b.Run.SampleHash == after.SampleHash
	var speedup float64
	if after.ElapsedS > 0 {
		speedup = math.Round((b.Run.ElapsedS/after.ElapsedS)*10000) / 10000
	}

	var notes []string
	outcome := domain.RecheckNoChange
	switch {
	case !rowsPreserved:
		outcome = domain.RecheckBehaviorChanged
		notes = append(notes, fmt.Sprintf(
			"Result set changed after the applied change (rows old=%d new=%d) — the change is UNSAFE, not just unhelpful.",
			b.Run.RowCount, after.RowCount))
	case speedup > 1.05:
		outcome = domain.RecheckImproved
		notes = append(notes, fmt.Sprintf("Measured %.4fs -> %.4fs (%.2fx) after the change.", b.Run.ElapsedS, after.ElapsedS, speedup))
	case speedup < 0.95:
		outcome = domain.RecheckWorse
		notes = append(notes, fmt.Sprintf("Slower after the change: %.4fs -> %.4fs.", b.Run.ElapsedS, after.ElapsedS))
	default:
		notes = append(notes, "No measurable timing change after the applied change.")
	}

	return domain.RecheckResult{
		Outcome:       outcome,
		Before:        b.Run,
		After:         after,
		BeforePlan:    b.Plan,
		AfterPlan:     afterPlan,
		Speedup:       speedup,
		RowsPreserved: rowsPreserved,
		Notes:         notes,
	}, nil
}
