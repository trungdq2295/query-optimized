package domain

// Stage names a phase of an Optimize run, emitted as progress so a frontend can
// show live steps (diagnose -> propose -> verify) instead of a frozen spinner.
type Stage string

const (
	// StageDiagnosing — running EXPLAIN to read the current plan.
	StageDiagnosing Stage = "diagnosing"
	// StageProposing — asking the proposer for a Proposal (may repeat on retry).
	StageProposing Stage = "proposing"
	// StageVerifying — proving the Proposal against the live DB.
	StageVerifying Stage = "verifying"
	// StageBaseline — capturing a "before" snapshot for an escalated change.
	StageBaseline Stage = "baseline"
	// StageDone — the run finished; the final result follows.
	StageDone Stage = "done"
)

// Progress is one step event in an Optimize run. Attempt is 1-based for the
// propose/verify phases. It carries no result payload — the final
// OptimizeResult is delivered separately when the run completes.
type Progress struct {
	Stage   Stage  `json:"stage"`
	Attempt int    `json:"attempt,omitempty"`
	Message string `json:"message,omitempty"`
}
