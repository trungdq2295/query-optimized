// Package database implements port.QueryRunner over database/sql. It is the
// only layer that executes SQL. Every execution passes through the injected
// SafetyValidator first, so the read-only guarantee holds no matter who calls.
//
// A Repo is bound to ONE engine (one driver + DSN). The engine name in method
// calls is asserted against that binding.
package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/datastore-engineering/query-optimizer/internal/domain"
	"github.com/datastore-engineering/query-optimizer/internal/port"
)

const defaultSampleN = 200

// Repo is the database-backed QueryRunner.
type Repo struct {
	db        *sql.DB
	ad        adapter
	validator port.SafetyValidator
}

// New binds a Repo to an open *sql.DB for the given engine.
func New(db *sql.DB, engine string, validator port.SafetyValidator) (*Repo, error) {
	ad, ok := lookup(engine)
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %v)", domain.ErrUnknownEngine, engine, names())
	}
	return &Repo{db: db, ad: ad, validator: validator}, nil
}

// EngineExists reports whether an adapter is registered (independent of binding).
func (r *Repo) EngineExists(engine string) bool {
	_, ok := lookup(engine)
	return ok
}

func (r *Repo) assertEngine(engine string) error {
	if engine != "" && engine != r.ad.name() {
		return fmt.Errorf("%w: repo bound to %q, got %q", domain.ErrUnknownEngine, r.ad.name(), engine)
	}
	return nil
}

// Run executes a SELECT under a timeout and returns its fingerprint.
func (r *Repo) Run(ctx context.Context, engine, query string, timeoutS int) (domain.RunResult, error) {
	if err := r.assertEngine(engine); err != nil {
		return domain.RunResult{}, err
	}
	body, err := r.validator.AssertSafeSelect(query) // security boundary
	if err != nil {
		return domain.RunResult{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	conn, err := r.db.Conn(runCtx)
	if err != nil {
		return domain.RunResult{}, err
	}
	defer conn.Close()
	if err := r.ad.applySessionGuards(runCtx, conn, timeoutS); err != nil {
		return domain.RunResult{}, fmt.Errorf("session guards: %w", err)
	}

	start := time.Now()
	rows, err := conn.QueryContext(runCtx, body)
	if err != nil {
		return domain.RunResult{}, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return domain.RunResult{}, err
	}

	var sample []string
	count := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return domain.RunResult{}, err
		}
		if count < defaultSampleN {
			parts := make([]string, len(vals))
			for i, v := range vals {
				parts[i] = stringifyCell(v)
			}
			sample = append(sample, strings.Join(parts, "|"))
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return domain.RunResult{}, err
	}
	elapsed := time.Since(start).Seconds()

	// Sort the sample so ORDER BY noise doesn't cause a false mismatch.
	sort.Strings(sample)
	digest := sha256.Sum256([]byte(strings.Join(sample, "\n")))

	return domain.RunResult{
		ElapsedS:   round4(elapsed),
		RowCount:   count,
		SampleHash: hex.EncodeToString(digest[:])[:16],
	}, nil
}

// Explain returns the plan as text. analyze=true executes the query.
func (r *Repo) Explain(ctx context.Context, engine, query string, analyze bool, timeoutS int) (string, error) {
	if err := r.assertEngine(engine); err != nil {
		return "", err
	}
	body, err := r.validator.AssertSafeSelect(query)
	if err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	conn, err := r.db.Conn(runCtx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := r.ad.applySessionGuards(runCtx, conn, timeoutS); err != nil {
		return "", fmt.Errorf("session guards: %w", err)
	}

	rows, err := conn.QueryContext(runCtx, r.ad.explainSQL(body, analyze))
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var lines []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = stringifyCell(v)
		}
		lines = append(lines, strings.Join(parts, " | "))
	}
	return strings.Join(lines, "\n"), rows.Err()
}

// SupportsManualIndex / SupportsHypotheticalIndex expose engine capability to
// the advisory verifiers (used to phrase index advice honestly). They resolve
// by name so they work even for engines other than the bound one.
func (r *Repo) SupportsManualIndex(engine string) bool {
	if a, ok := lookup(engine); ok {
		return a.supportsManualIndex()
	}
	return r.ad.supportsManualIndex()
}

func (r *Repo) SupportsHypotheticalIndex(engine string) bool {
	if a, ok := lookup(engine); ok {
		return a.supportsHypotheticalIndex()
	}
	return r.ad.supportsHypotheticalIndex()
}

// EngineName returns the bound engine's name.
func (r *Repo) EngineName() string { return r.ad.name() }

func stringifyCell(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
