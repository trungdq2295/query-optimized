// Command qopt is the CLI entry point. It blank-imports the database drivers
// (the ONLY place that does — see DECISIONS.md ADR-0005) so they register with
// database/sql, then hands control to the cli handler.
package main

import (
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"github.com/trudoan/query-optimizer/internal/handler/cli"
)

func main() {
	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}
