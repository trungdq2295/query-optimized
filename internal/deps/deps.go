// Package deps is the composition root: it wires concretes into the ports and
// builds the use case. It is the ONLY package that knows how the layers fit
// together. Swapping an implementation (e.g. a different QueryRunner) is a
// one-line change here.
package deps

import (
	"database/sql"
	"fmt"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/repo/database"
	"github.com/datastore-engineering/query-optimizer/internal/service/safety"
	"github.com/datastore-engineering/query-optimizer/internal/service/verification"
	"github.com/datastore-engineering/query-optimizer/internal/usecase/optimize"
)

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
func BuildUseCase(db *sql.DB, engine string) (*optimize.UseCase, error) {
	validator := safety.New()
	repo, err := database.New(db, engine, validator)
	if err != nil {
		return nil, err
	}
	verifier := verification.New(repo)
	return optimize.New(repo, verifier), nil
}
