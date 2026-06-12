// Command qopt-seed loads a demo dataset into a database so the optimizer has
// real data to prove speedups against. It runs a plain .sql file through the
// registered drivers — so a fresh clone needs NO sqlite3/mysql client binary,
// only this Go tool. It is a dev/demo utility, separate from the read-only
// query path (it intentionally executes DDL + INSERTs).
//
//	qopt-seed -engine sqlite -dsn ./qopt-demo.db -file examples/seed-sqlite.sql
//	qopt-seed -engine mysql  -dsn "$QOPT_DSN"    -file examples/seed-mysql.sql
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"github.com/trudoan/query-optimizer/internal/deps"
)

func main() {
	engine := flag.String("engine", "sqlite", "engine: sqlite|mysql")
	dsn := flag.String("dsn", "", "connection string (sqlite: a file path)")
	file := flag.String("file", "", "path to the .sql seed file")
	flag.Parse()

	if *dsn == "" || *file == "" {
		log.Fatal("usage: qopt-seed -engine <e> -dsn <dsn> -file <seed.sql>")
	}

	script, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read seed file: %v", err)
	}

	db, err := deps.OpenDB(*engine, *dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	stmts := splitStatements(string(script))
	for i, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			log.Fatalf("statement %d failed: %v\n--- sql ---\n%s", i+1, err, s)
		}
	}
	fmt.Printf("seeded %s from %s (%d statements)\n", *engine, *file, len(stmts))
}

// splitStatements breaks a seed script into individual statements. It strips
// whole-line "--" comments FIRST (they may contain ';', which would otherwise
// split mid-comment), then splits the remaining SQL on ';'. The seed files we
// ship have no semicolons inside string literals, so this is correct and keeps
// the tool dependency-free.
func splitStatements(script string) []string {
	var code []string
	for _, line := range strings.Split(script, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		code = append(code, line)
	}

	var out []string
	for _, raw := range strings.Split(strings.Join(code, "\n"), ";") {
		if stmt := strings.TrimSpace(raw); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}
