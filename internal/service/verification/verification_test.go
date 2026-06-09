package verification

import (
	"context"
	"testing"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
)

// fakeRunner is a stand-in QueryRunner: it returns canned RunResults keyed by
// SQL, so verifier logic is tested with no database.
type fakeRunner struct {
	results map[string]domain.RunResult
	manual  bool
	hypo    bool
}

func (f *fakeRunner) Run(_ context.Context, _, sql string, _ int) (domain.RunResult, error) {
	return f.results[sql], nil
}
func (f *fakeRunner) Explain(_ context.Context, _, _ string, _ bool, _ int) (string, error) {
	return "", nil
}
func (f *fakeRunner) EngineExists(string) bool                { return true }
func (f *fakeRunner) SupportsManualIndex(string) bool         { return f.manual }
func (f *fakeRunner) SupportsHypotheticalIndex(string) bool   { return f.hypo }

func TestRewrite_Verified(t *testing.T) {
	fr := &fakeRunner{results: map[string]domain.RunResult{
		"OLD": {ElapsedS: 10, RowCount: 100, SampleHash: "abc"},
		"NEW": {ElapsedS: 1, RowCount: 100, SampleHash: "abc"}, // same data, 10x faster
	}}
	svc := New(fr)
	v, err := svc.Verify(context.Background(), domain.Proposal{
		Kind: domain.KindRewrite, OriginalSQL: "OLD", RewrittenSQL: "NEW",
	}, "mysql", 30)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != domain.StatusVerified {
		t.Fatalf("want verified, got %q", v.Status)
	}
	if v.Speedup == nil || *v.Speedup != 10 {
		t.Fatalf("want 10x speedup, got %v", v.Speedup)
	}
}

func TestRewrite_BehaviorChanged(t *testing.T) {
	fr := &fakeRunner{results: map[string]domain.RunResult{
		"OLD": {ElapsedS: 10, RowCount: 100, SampleHash: "abc"},
		"NEW": {ElapsedS: 1, RowCount: 90, SampleHash: "xyz"}, // different rows!
	}}
	svc := New(fr)
	v, _ := svc.Verify(context.Background(), domain.Proposal{
		Kind: domain.KindRewrite, OriginalSQL: "OLD", RewrittenSQL: "NEW",
	}, "mysql", 30)
	if v.Status != domain.StatusBehaviorChanged {
		t.Fatalf("want behavior_changed, got %q", v.Status)
	}
	if v.BehaviorPreserved == nil || *v.BehaviorPreserved {
		t.Fatal("want behavior NOT preserved")
	}
}

func TestRewrite_NotFaster(t *testing.T) {
	fr := &fakeRunner{results: map[string]domain.RunResult{
		"OLD": {ElapsedS: 1, RowCount: 100, SampleHash: "abc"},
		"NEW": {ElapsedS: 2, RowCount: 100, SampleHash: "abc"}, // same data, slower
	}}
	svc := New(fr)
	v, _ := svc.Verify(context.Background(), domain.Proposal{
		Kind: domain.KindRewrite, OriginalSQL: "OLD", RewrittenSQL: "NEW",
	}, "mysql", 30)
	if v.Status != domain.StatusNotFaster {
		t.Fatalf("want not_faster, got %q", v.Status)
	}
}

func TestIndex_Unverifiable(t *testing.T) {
	svc := New(&fakeRunner{manual: true})
	v, _ := svc.Verify(context.Background(), domain.Proposal{
		Kind: domain.KindIndex, DDL: "CREATE INDEX ...",
	}, "mysql", 30)
	if v.Status != domain.StatusUnverifiable {
		t.Fatalf("want unverifiable, got %q", v.Status)
	}
}

func TestUnknownKind(t *testing.T) {
	svc := New(&fakeRunner{})
	_, err := svc.Verify(context.Background(), domain.Proposal{Kind: "bogus"}, "mysql", 30)
	if err == nil {
		t.Fatal("want error for unknown kind")
	}
}
