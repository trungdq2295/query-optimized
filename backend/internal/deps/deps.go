// Package deps is the composition root: it wires concretes into the ports and
// builds the use case. It is the ONLY package that knows how the layers fit
// together. Swapping an implementation (e.g. a different QueryRunner) is a
// one-line change here.
package deps

import (
	"database/sql"
	"fmt"

	"github.com/trudoan/query-optimizer/internal/domain"
	"github.com/trudoan/query-optimizer/internal/port"
	"github.com/trudoan/query-optimizer/internal/repo/baseline"
	"github.com/trudoan/query-optimizer/internal/repo/database"
	"github.com/trudoan/query-optimizer/internal/service/propose/api"
	"github.com/trudoan/query-optimizer/internal/service/propose/cli"
	"github.com/trudoan/query-optimizer/internal/service/safety"
	"github.com/trudoan/query-optimizer/internal/service/verification"
	"github.com/trudoan/query-optimizer/internal/usecase/optimize"
)

// Mode selects which proposer the server uses. It is the one switch that turns
// the same backend into a local-CLI tool or a hosted webapp.
const (
	// ModeLocal — spawn the user's own installed agent CLI (claude/cursor). No
	// server-side API key; the user's CLI login pays for the model.
	ModeLocal = "local"
	// ModeHosted — the server calls an LLM API with its OWN key against one
	// fixed demo database. For a public webapp where users have no CLI.
	ModeHosted = "hosted"
	// ModeVerify — no proposer; diagnose + verify a hand-written proposal only.
	ModeVerify = "verify"
)

// BuildProposer picks the proposer for a mode. Local and hosted return a real
// proposer (and surface a clear error if their prerequisite — a CLI on PATH, or
// QOPT_API_KEY — is missing). Verify mode returns nil: a pure verify frontend.
func BuildProposer(mode string) (port.Proposer, error) {
	switch mode {
	case ModeLocal:
		return cli.New()
	case ModeHosted:
		return api.New()
	case ModeVerify, "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown QOPT_MODE %q (want local|hosted|verify)", mode)
	}
}

// engineToDriver maps an engine name to the registered database/sql driver.
// The driver is blank-imported in cmd/qopt (side-effect registration). Engines
// without a driver here fail clearly at OpenDB — add the import + a row to enable.
var engineToDriver = map[string]string{
	"mysql":  "mysql",
	"sqlite": "sqlite",
}

// OpenDB opens a *sql.DB for the engine. The DSN is a secret — errors from
// sql.Open do not include it.
func OpenDB(engine, dsn string) (*sql.DB, error) {
	driver, ok := engineToDriver[engine]
	if !ok {
		return nil, fmt.Errorf("%w: %q has no imported driver (add it in deps)", domain.ErrUnknownEngine, engine)
	}
	return sql.Open(driver, dsn)
}

// BuildUseCase wires safety -> repo -> verification -> usecase for one engine.
// proposer is optional: a pure verify/diagnose frontend (the CLI) passes nil;
// the web backend passes a cliProposer (local mode) or apiProposer (hosted).
// The baseline store is always wired — it has no external dependency.
func BuildUseCase(db *sql.DB, engine string, proposer port.Proposer) (*optimize.UseCase, error) {
	validator := safety.New()
	repo, err := database.New(db, engine, validator)
	if err != nil {
		return nil, err
	}
	verifier := verification.New(repo)
	baselines := baseline.New()
	return optimize.New(repo, verifier, proposer, baselines), nil
}
