// Package verification implements port.VerificationService. It owns the
// per-OptKind verifier strategies and dispatches a Proposal to the one matching
// its Kind. Adding a kind = a new Verifier + one registry entry here.
package verification

import (
	"context"
	"fmt"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/port"
)

// Service dispatches proposals to per-kind verifiers.
type Service struct {
	verifiers map[domain.OptKind]port.Verifier
}

// New wires every verifier strategy. The QueryRunner is injected into the ones
// that execute (rewrite) or query engine capability (index).
func New(runner port.QueryRunner) *Service {
	s := &Service{verifiers: map[domain.OptKind]port.Verifier{}}
	s.register(NewRewriteVerifier(runner))
	s.register(NewIndexVerifier(runner))
	s.register(NewPartitionVerifier())
	s.register(NewShardVerifier())
	return s
}

func (s *Service) register(v port.Verifier) { s.verifiers[v.Kind()] = v }

// Verify dispatches by proposal.Kind. The caller never knows the per-kind rules.
func (s *Service) Verify(ctx context.Context, p domain.Proposal, engine string, timeoutS int) (domain.Verdict, error) {
	v, ok := s.verifiers[p.Kind]
	if !ok {
		return domain.Verdict{}, fmt.Errorf("%w: %q", domain.ErrNoVerifier, p.Kind)
	}
	return v.Verify(ctx, p, engine, timeoutS)
}
