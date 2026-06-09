// Package domain holds the core entities of the query optimizer. It has no
// dependencies on any other layer — everything points inward to here.
package domain

// OptKind is the category of optimization a Proposal carries. The category
// decides how it gets verified and whether it is self-serve or must escalate.
type OptKind string

const (
	// KindRewrite — a behavior-preserving SQL rewrite. Self-serve: provable by
	// running old vs new and comparing results + timing.
	KindRewrite OptKind = "rewrite"
	// KindIndex — a CREATE INDEX recommendation. Propose-only: the tool never
	// runs DDL, so the speedup cannot be proven without the index existing.
	KindIndex OptKind = "index"
	// KindPartition — a partition-key suggestion. Propose-only, DBA approves.
	KindPartition OptKind = "partition"
	// KindShard — detected need to shard. Detect-only: escalate to data eng.
	KindShard OptKind = "shard"
)

// Proposal is what the transform layer (the AI, outside this binary) hands to
// the verify layer. One Proposal = one optimization. It is produced as JSON:
// the AI reads EXPLAIN + the rules doc and emits this shape.
type Proposal struct {
	Kind         OptKind `json:"kind"`
	OriginalSQL  string  `json:"original_sql"`
	RewrittenSQL string  `json:"rewritten_sql,omitempty"` // KindRewrite only
	DDL          string  `json:"ddl,omitempty"`           // KindIndex/Partition; text, NEVER executed
	Rationale    string  `json:"rationale"`               // plain-language why
	SelfServe    bool    `json:"self_serve"`              // rewrite=true; others=false
}
