package domain

// ProposeInput is what the proposer (the AI half) needs to produce a Proposal:
// the slow query, its EXPLAIN plan, the engine, and — on a retry — the prior
// attempt's rewrite + verdict so it can correct itself (the agent-in-the-loop).
type ProposeInput struct {
	Engine      string
	OriginalSQL string
	Plan        string // EXPLAIN output from the diagnose step

	// Retry feedback. Attempt is 0 on the first try. PriorRewrite/PriorVerdict
	// are set when a previous Proposal failed verification, so the proposer can
	// see WHY (behavior_changed / not_faster) and try again.
	Attempt      int
	PriorRewrite string
	PriorVerdict *Verdict
}

// OptimizeResult is the full outcome of an optimize run: the proposal the AI
// produced and the verdict the deterministic verifier returned. Pointer Verdict
// is nil only on an internal error before verification.
type OptimizeResult struct {
	Proposal   Proposal `json:"proposal"`
	Verdict    Verdict  `json:"verdict"`
	Engine     string   `json:"engine"`      // which DB this ran against
	Attempts   int      `json:"attempts"`    // how many propose->verify cycles
	Proposer   string   `json:"proposer"`    // name of the proposer used (cli:claude / api / ...)
	BaselineID string   `json:"baseline_id,omitempty"` // set when a baseline was captured
}
