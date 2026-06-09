package deps_test

// End-to-end proof of the WHOLE wiring (deps -> repo -> verification -> usecase)
// against a real database, offline. Uses modernc.org/sqlite (pure Go: no cgo,
// no server, no network). If the layers stop fitting together, this fails.

import (
	"context"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/datastore-engineering/query-optimizer/internal/deps"
	"github.com/datastore-engineering/query-optimizer/internal/domain"
)

func setup(t *testing.T) (*context.Context, func(domain.Proposal) domain.Verdict) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := deps.OpenDB("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, cust_id INTEGER, amount INTEGER, status TEXT)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	for i := 1; i <= 1000; i++ {
		status := "open"
		if i%3 == 0 {
			status = "closed"
		}
		if _, err := db.Exec(`INSERT INTO orders VALUES (?, ?, ?, ?)`, i, i%50, i*10, status); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	uc, err := deps.BuildUseCase(db, "sqlite")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ctx := context.Background()
	verify := func(p domain.Proposal) domain.Verdict {
		v, err := uc.Verify(ctx, p, "sqlite", 30)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return v
	}
	return &ctx, verify
}

func TestE2E_RewritePreservesBehavior(t *testing.T) {
	_, verify := setup(t)
	v := verify(domain.Proposal{
		Kind:         domain.KindRewrite,
		OriginalSQL:  `SELECT id, amount FROM orders WHERE status = 'open' AND amount > 100 ORDER BY id`,
		RewrittenSQL: `SELECT id, amount FROM orders WHERE amount > 100 AND status = 'open' ORDER BY id`,
		SelfServe:    true,
	})
	if v.BehaviorPreserved == nil || !*v.BehaviorPreserved {
		t.Fatalf("expected behavior preserved, got %+v", v)
	}
	if v.Status != domain.StatusVerified && v.Status != domain.StatusNotFaster {
		t.Fatalf("expected verified/not_faster, got %q", v.Status)
	}
}

func TestE2E_RewriteChangesBehavior(t *testing.T) {
	_, verify := setup(t)
	v := verify(domain.Proposal{
		Kind:         domain.KindRewrite,
		OriginalSQL:  `SELECT id FROM orders WHERE status = 'open' ORDER BY id`,
		RewrittenSQL: `SELECT id FROM orders WHERE status = 'closed' ORDER BY id`,
		SelfServe:    true,
	})
	if v.Status != domain.StatusBehaviorChanged {
		t.Fatalf("expected behavior_changed, got %q", v.Status)
	}
}

func TestE2E_RejectsWriteSmuggle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db, _ := deps.OpenDB("sqlite", path)
	defer db.Close()
	db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY)`)
	uc, _ := deps.BuildUseCase(db, "sqlite")
	_, err := uc.Verify(context.Background(), domain.Proposal{
		Kind:         domain.KindRewrite,
		OriginalSQL:  `SELECT id FROM orders`,
		RewrittenSQL: `SELECT id FROM orders; DROP TABLE orders`,
	}, "sqlite", 30)
	if err == nil {
		t.Fatal("expected error on write smuggle")
	}
}
