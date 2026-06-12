package domain

import "time"

// Baseline is a "before" snapshot of a query, captured at diagnose time. It
// exists so a system change the tool CANNOT apply (index/partition — DDL) can
// still be PROVEN after an engineer applies it: re-run the same query and diff
// against this snapshot. No prediction — measured before vs measured after.
type Baseline struct {
	ID         string    `json:"id"`
	Engine     string    `json:"engine"`
	SQL        string    `json:"sql"`
	Plan       string    `json:"plan"` // EXPLAIN text at capture time
	Run        RunResult `json:"run"`  // timed run fingerprint at capture time
	CapturedAt time.Time `json:"captured_at"`
}

// RecheckOutcome is the verdict of a verify-after-applied recheck.
type RecheckOutcome string

const (
	// RecheckImproved — the same query is measurably faster after the change.
	RecheckImproved RecheckOutcome = "improved"
	// RecheckNoChange — no measurable improvement.
	RecheckNoChange RecheckOutcome = "no_change"
	// RecheckWorse — the change made it slower.
	RecheckWorse RecheckOutcome = "worse"
	// RecheckBehaviorChanged — the result set changed (e.g. a bad partition);
	// the change is unsafe, not just unhelpful.
	RecheckBehaviorChanged RecheckOutcome = "behavior_changed"
)

// RecheckResult is the measured before/after for a baseline. All numbers are
// real runs — nothing is predicted.
type RecheckResult struct {
	Outcome     RecheckOutcome `json:"outcome"`
	Before      RunResult      `json:"before"`
	After       RunResult      `json:"after"`
	BeforePlan  string         `json:"before_plan"`
	AfterPlan   string         `json:"after_plan"`
	Speedup     float64        `json:"speedup"` // before.ElapsedS / after.ElapsedS
	RowsPreserved bool         `json:"rows_preserved"`
	Notes       []string       `json:"notes"`
}
