package optimize

import (
	"context"
	"testing"

	"github.com/trudoan/query-optimizer/internal/domain"
)

// fakes for the two ports — the usecase is pure orchestration, so we just assert
// it forwards to the right port.

type fakeRunner struct{ plan string }

func (f fakeRunner) Run(context.Context, string, string, int) (domain.RunResult, error) {
	return domain.RunResult{}, nil
}
func (f fakeRunner) Explain(context.Context, string, string, bool, int) (string, error) {
	return f.plan, nil
}
func (fakeRunner) EngineExists(string) bool              { return true }
func (fakeRunner) SupportsManualIndex(string) bool       { return true }
func (fakeRunner) SupportsHypotheticalIndex(string) bool { return false }

type fakeVerification struct{ status domain.VerdictStatus }

func (f fakeVerification) Verify(context.Context, domain.Proposal, string, int) (domain.Verdict, error) {
	return domain.Verdict{Status: f.status}, nil
}

func TestExplain_Forwards(t *testing.T) {
	uc := New(fakeRunner{plan: "PLAN-TEXT"}, fakeVerification{}, nil, nil)
	got, err := uc.Explain(context.Background(), "mysql", "SELECT 1", false, 30)
	if err != nil || got != "PLAN-TEXT" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestVerify_Forwards(t *testing.T) {
	uc := New(fakeRunner{}, fakeVerification{status: domain.StatusVerified}, nil, nil)
	v, err := uc.Verify(context.Background(), domain.Proposal{Kind: domain.KindRewrite}, "mysql", 30)
	if err != nil || v.Status != domain.StatusVerified {
		t.Fatalf("got %+v err %v", v, err)
	}
}
