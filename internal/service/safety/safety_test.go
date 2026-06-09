package safety

import (
	"errors"
	"testing"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
)

func TestAssertSafeSelect_Accepts(t *testing.T) {
	v := New()
	ok := []string{
		"SELECT 1",
		"select * from orders where id = 5",
		"  SELECT a, b FROM t  ",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- a comment\nSELECT 1",
		"/* block */ SELECT 1",
		"SELECT 1;",
		"select count(*) from customers c join orders o on o.cust_id = c.id",
	}
	for _, q := range ok {
		if _, err := v.AssertSafeSelect(q); err != nil {
			t.Errorf("expected accept, got error for %q: %v", q, err)
		}
	}
}

func TestAssertSafeSelect_Rejects(t *testing.T) {
	v := New()
	bad := []string{
		"", "   ", ";",
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"CREATE INDEX i ON t(a)",
		"TRUNCATE t",
		"SELECT 1; DROP TABLE t",
		"SELECT 1; SELECT 2",
		"GRANT ALL ON t TO u",
		"VACUUM",
		"WITH x AS (SELECT 1) DELETE FROM y",
	}
	for _, q := range bad {
		_, err := v.AssertSafeSelect(q)
		if err == nil {
			t.Errorf("expected REJECT, but accepted: %q", q)
			continue
		}
		if !errors.Is(err, domain.ErrUnsafeSQL) {
			t.Errorf("expected ErrUnsafeSQL for %q, got %v", q, err)
		}
	}
}
