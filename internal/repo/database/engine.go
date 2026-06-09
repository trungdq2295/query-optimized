package database

// engine.go — infrastructure detail: per-engine SQL dialect. This knowledge
// (how to phrase EXPLAIN, how to set read-only + a timeout, whether manual
// indexes exist) is hidden inside the repo and never leaks to upper layers.
// The QueryRunner exposes only an engine name; it resolves the adapter here.
//
// Adding an engine = a struct + one register() call in init(). No upper layer moves.

import (
	"context"
	"database/sql"
	"fmt"
)

// adapter is everything the repo needs to know about a specific SQL engine.
type adapter interface {
	name() string
	applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error
	explainSQL(body string, analyze bool) string
	supportsManualIndex() bool
	supportsHypotheticalIndex() bool
}

var registry = map[string]adapter{}

func register(a adapter) { registry[a.name()] = a }

func lookup(name string) (adapter, bool) {
	a, ok := registry[name]
	return a, ok
}

func names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	return out
}

// --- mysql (PRIMARY, the engine proven live) -------------------------------

type mysqlAdapter struct{}

func (mysqlAdapter) name() string { return "mysql" }

func (mysqlAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error {
	ms := timeoutS * 1000
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time = %d", ms)); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, "SET SESSION transaction_read_only = 1")
	return err
}

func (mysqlAdapter) explainSQL(body string, analyze bool) string {
	if analyze {
		return "EXPLAIN ANALYZE " + body
	}
	return "EXPLAIN FORMAT=TREE " + body
}

func (mysqlAdapter) supportsManualIndex() bool       { return true }
func (mysqlAdapter) supportsHypotheticalIndex() bool { return false }

// --- sqlite (offline tests) ------------------------------------------------

type sqliteAdapter struct{}

func (sqliteAdapter) name() string { return "sqlite" }

func (sqliteAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error {
	_, _ = ctx, timeoutS // no server-side timeout; context deadline guards
	return nil
}

func (sqliteAdapter) explainSQL(body string, analyze bool) string {
	_ = analyze
	return "EXPLAIN QUERY PLAN " + body
}

func (sqliteAdapter) supportsManualIndex() bool       { return true }
func (sqliteAdapter) supportsHypotheticalIndex() bool { return false }

// --- postgres --------------------------------------------------------------

type postgresAdapter struct{}

func (postgresAdapter) name() string { return "postgres" }

func (postgresAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error {
	ms := timeoutS * 1000
	if _, err := conn.ExecContext(ctx, "SET default_transaction_read_only = on"); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, fmt.Sprintf("SET statement_timeout = %d", ms))
	return err
}

func (postgresAdapter) explainSQL(body string, analyze bool) string {
	if analyze {
		return "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) " + body
	}
	return "EXPLAIN " + body
}

func (postgresAdapter) supportsManualIndex() bool       { return true }
func (postgresAdapter) supportsHypotheticalIndex() bool { return true } // HypoPG

// --- mssql -----------------------------------------------------------------

type mssqlAdapter struct{}

func (mssqlAdapter) name() string { return "mssql" }

func (mssqlAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error {
	_, err := conn.ExecContext(ctx, fmt.Sprintf("SET LOCK_TIMEOUT %d", timeoutS*1000))
	return err
}

// Real MSSQL plan output needs SET SHOWPLAN_* toggling — placeholder for now.
func (mssqlAdapter) explainSQL(body string, analyze bool) string {
	_ = analyze
	return body
}

func (mssqlAdapter) supportsManualIndex() bool       { return true }
func (mssqlAdapter) supportsHypotheticalIndex() bool { return false }

// --- snowflake (NO manual indexes — advice redirects to clustering) --------

type snowflakeAdapter struct{}

func (snowflakeAdapter) name() string { return "snowflake" }

func (snowflakeAdapter) applySessionGuards(ctx context.Context, conn *sql.Conn, timeoutS int) error {
	_, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS = %d", timeoutS))
	return err
}

func (snowflakeAdapter) explainSQL(body string, analyze bool) string {
	_ = analyze
	return "EXPLAIN USING TEXT " + body
}

func (snowflakeAdapter) supportsManualIndex() bool       { return false }
func (snowflakeAdapter) supportsHypotheticalIndex() bool { return false }

func init() {
	register(mysqlAdapter{})
	register(sqliteAdapter{})
	register(postgresAdapter{})
	register(mssqlAdapter{})
	register(snowflakeAdapter{})
}
