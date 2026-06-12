// Command qopt-server is the web entry point. It blank-imports the database
// drivers (like cmd/qopt) then starts the HTTP delivery adapter. ONE binary
// serves both modes — set QOPT_MODE=local (spawn the user's CLI) or hosted
// (server-side API key). The connection string is a server secret (QOPT_DSN);
// request bodies never carry it.
//
// Env:
//
//	QOPT_MODE         local | hosted | verify   (default local)
//	QOPT_ENGINE       mysql | sqlite | ...       (default sqlite)
//	QOPT_DSN          connection string          (secret, required)
//	QOPT_ADDR         listen address             (default :8080)
//	QOPT_CORS_ORIGIN  CORS allow-origin          (default *)
//	QOPT_TIMEOUT      per-query timeout seconds  (default 60)
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"github.com/trudoan/query-optimizer/internal/deps"
	httphandler "github.com/trudoan/query-optimizer/internal/handler/http"
)

func main() {
	mode := env("QOPT_MODE", deps.ModeLocal)
	engine := env("QOPT_ENGINE", "sqlite")
	dsn := os.Getenv("QOPT_DSN")
	addr := env("QOPT_ADDR", ":8080")
	origin := env("QOPT_CORS_ORIGIN", "*")
	timeoutS := envInt("QOPT_TIMEOUT", 60)

	if dsn == "" {
		log.Fatal("QOPT_DSN is required (the database connection string)")
	}

	proposer, err := deps.BuildProposer(mode)
	if err != nil {
		log.Fatalf("proposer (mode=%s): %v", mode, err)
	}

	db, err := deps.OpenDB(engine, dsn) // error never contains the DSN
	if err != nil {
		log.Fatalf("open db (engine=%s): %v", engine, err)
	}
	defer db.Close()

	uc, err := deps.BuildUseCase(db, engine, proposer)
	if err != nil {
		log.Fatalf("build use case: %v", err)
	}

	srv := httphandler.New(uc, engine, mode, origin, timeoutS)
	log.Printf("qopt-server listening on %s (mode=%s engine=%s)", addr, mode, engine)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
