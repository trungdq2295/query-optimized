package domain

// VerdictStatus is the outcome of verification.
type VerdictStatus string

const (
	// StatusVerified — faster AND behavior-preserving. Safe to present.
	StatusVerified VerdictStatus = "verified"
	// StatusBehaviorChanged — returns different rows. Reject the rewrite.
	StatusBehaviorChanged VerdictStatus = "behavior_changed"
	// StatusNotFaster — same or worse timing. Not worth presenting as a win.
	StatusNotFaster VerdictStatus = "not_faster"
	// StatusUnverifiable — cannot be proven without DDL (index/partition) or
	// without a DB (offline). Advice stands as a prediction, clearly labeled.
	StatusUnverifiable VerdictStatus = "unverifiable"
)

// RunResult is the fingerprint of a single query execution. It lets old vs new
// be compared without holding entire result sets in memory.
type RunResult struct {
	ElapsedS   float64 `json:"elapsed_s"`
	RowCount   int     `json:"row_count"`
	SampleHash string  `json:"sample_hash"` // sha256 of sorted first-N rows, 16 hex chars
}

// Verdict is what the verify layer hands back. Pointer fields are nil when the
// proposal could not be verified (e.g. index advice with no DDL).
type Verdict struct {
	Status            VerdictStatus `json:"status"`
	BehaviorPreserved *bool         `json:"behavior_preserved,omitempty"`
	Speedup           *float64      `json:"speedup,omitempty"` // old.ElapsedS / new.ElapsedS
	Old               *RunResult    `json:"old,omitempty"`
	New               *RunResult    `json:"new,omitempty"`
	Notes             []string      `json:"notes"`
}

// BoolPtr / FloatPtr — helpers for setting optional Verdict fields.
func BoolPtr(b bool) *bool        { return &b }
func FloatPtr(f float64) *float64 { return &f }
