// Package cli is a delivery adapter: it turns command-line input into use-case
// calls and renders the result. A Slack or web handler would be a sibling of
// this package, reusing the same use case and contracts.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/trudoan/query-optimizer/internal/deps"
	"github.com/trudoan/query-optimizer/internal/domain"
)

// Run parses args, builds the use case, and dispatches. args is os.Args.
func Run(args []string) error {
	if len(args) < 2 {
		usage()
		return fmt.Errorf("missing command")
	}
	cmd := args[1]

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	engine := fs.String("engine", "mysql", "engine: mysql|sqlite|postgres|mssql|snowflake")
	dsn := fs.String("dsn", os.Getenv("QOPT_DSN"), "connection string (or env QOPT_DSN). SECRET — never logged.")
	sqlText := fs.String("sql", "", "SQL (explain only)")
	analyze := fs.Bool("analyze", false, "EXPLAIN ANALYZE — EXECUTES the query (explain only)")
	timeout := fs.Int("timeout", 60, "per-query statement timeout, seconds")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	if *dsn == "" {
		return fmt.Errorf("no connection string: pass -dsn or set QOPT_DSN")
	}

	db, err := deps.OpenDB(*engine, *dsn) // err does not contain the DSN
	if err != nil {
		return err
	}
	defer db.Close()

	uc, err := deps.BuildUseCase(db, *engine, nil) // CLI only diagnoses + verifies
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch cmd {
	case "explain":
		if *sqlText == "" {
			return fmt.Errorf("explain needs -sql")
		}
		plan, err := uc.Explain(ctx, *engine, *sqlText, *analyze, *timeout)
		if err != nil {
			return err
		}
		fmt.Println(plan)
		return nil

	case "verify":
		raw, err := io.ReadAll(os.Stdin) // AI writes the Proposal JSON to stdin
		if err != nil {
			return err
		}
		var p domain.Proposal
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("parse Proposal JSON: %w", err)
		}
		v, err := uc.Verify(ctx, p, *engine, *timeout)
		if err != nil {
			return err
		}
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return nil

	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `qopt — query-optimizer verify engine

Usage:
  qopt explain -engine mysql -dsn <dsn> -sql "SELECT ..." [-analyze]
  echo '<Proposal JSON>' | qopt verify -engine mysql -dsn <dsn>

The connection string (-dsn / QOPT_DSN) is a secret and is never logged.
verify reads a JSON Proposal on stdin and prints a JSON Verdict.
`)
}
