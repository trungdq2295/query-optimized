package domain

import "errors"

// Domain-level sentinel errors. Layers wrap these so callers can branch without
// string-matching.
var (
	// ErrUnsafeSQL is returned by the safety boundary for any input that is not
	// a single read-only SELECT/WITH statement.
	ErrUnsafeSQL = errors.New("unsafe sql: only a single read-only SELECT/WITH is allowed")
	// ErrUnknownEngine is returned when no adapter is registered for an engine.
	ErrUnknownEngine = errors.New("unknown engine")
	// ErrNoVerifier is returned when no verifier handles a proposal's kind.
	ErrNoVerifier = errors.New("no verifier registered for kind")
	// ErrInvalidProposal is returned when a proposal is missing required fields.
	ErrInvalidProposal = errors.New("invalid proposal")
)
