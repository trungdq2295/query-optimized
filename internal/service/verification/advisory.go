package verification

import (
	"context"
	"fmt"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/port"
)

// The advisory verifiers cover the kinds that CANNOT be proven in-tool because
// proving them needs DDL (which this tool never runs) or a DBA. They return
// StatusUnverifiable with honest, actionable notes — never a fake "verified".

// IndexVerifier — index advice is a prediction, not a proof.
type IndexVerifier struct {
	runner port.QueryRunner
}

// NewIndexVerifier injects the runner to query engine capability (manual /
// hypothetical index support) so advice is phrased honestly per engine.
func NewIndexVerifier(runner port.QueryRunner) *IndexVerifier {
	return &IndexVerifier{runner: runner}
}

func (*IndexVerifier) Kind() domain.OptKind { return domain.KindIndex }

func (v *IndexVerifier) Verify(_ context.Context, _ domain.Proposal, engine string, _ int) (domain.Verdict, error) {
	notes := []string{
		"Index advice is a PREDICTION from plan cost, not a verified result — " +
			"proving it requires the index to exist (DDL), which this tool never runs.",
		"DA action: ask your DBA to apply the CREATE INDEX; do not run DDL yourself.",
	}
	switch {
	case !v.runner.SupportsManualIndex(engine):
		notes = append(notes, fmt.Sprintf(
			"Engine %q has no manual B-tree indexes — redirect to clustering / "+
				"partition pruning / rewrite instead.", engine))
	case v.runner.SupportsHypotheticalIndex(engine):
		notes = append(notes, "This engine supports hypothetical indexes (HypoPG) — "+
			"use it to confirm the plan improvement WITHOUT creating the index.")
	}
	return domain.Verdict{Status: domain.StatusUnverifiable, Notes: notes}, nil
}

// PartitionVerifier — DBA territory.
type PartitionVerifier struct{}

func NewPartitionVerifier() *PartitionVerifier { return &PartitionVerifier{} }

func (*PartitionVerifier) Kind() domain.OptKind { return domain.KindPartition }

func (*PartitionVerifier) Verify(_ context.Context, _ domain.Proposal, _ string, _ int) (domain.Verdict, error) {
	return domain.Verdict{
		Status: domain.StatusUnverifiable,
		Notes: []string{
			"Partitioning changes physical layout (DDL) — propose only; the DBA approves and applies.",
			"Cannot be verified in-tool without rebuilding the table.",
		},
	}, nil
}

// ShardVerifier — detect and escalate.
type ShardVerifier struct{}

func NewShardVerifier() *ShardVerifier { return &ShardVerifier{} }

func (*ShardVerifier) Kind() domain.OptKind { return domain.KindShard }

func (*ShardVerifier) Verify(_ context.Context, _ domain.Proposal, _ string, _ int) (domain.Verdict, error) {
	return domain.Verdict{
		Status: domain.StatusUnverifiable,
		Notes: []string{
			"Sharding is not self-serve — detected and ESCALATED to the data engineering team.",
			"This tool deliberately does not design sharding topologies.",
		},
	}, nil
}
