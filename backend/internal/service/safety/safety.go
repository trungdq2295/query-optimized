// Package safety implements port.SafetyValidator — THE security boundary that
// stops a "verify" from ever running a write or DDL. It is ported line-for-line
// from the proven Python guard (skills/query-optimizing/tool.py:
// assert_safe_select). Do NOT relax it casually; every relaxation widens what
// can hit the database.
//
// Rules (all must pass):
//  1. Non-empty after comments stripped.
//  2. Exactly ONE statement (no `;`-chained second statement).
//  3. First keyword is SELECT or WITH.
//  4. No forbidden write/DDL keyword anywhere in the body.
package safety

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/trudoan/query-optimizer/internal/domain"
)

// forbidden matches any write/DDL keyword as a whole word, case-insensitive.
// Identical keyword set to the Python guard. Accepted limitation: a keyword
// denylist over-rejects (fails safe) rather than under-rejects.
var forbidden = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|create|truncate|grant|revoke|replace|call|exec|execute|attach|copy|vacuum|reindex)\b`)

var (
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reLineComment  = regexp.MustCompile(`--[^\n]*`)
)

// Validator is the concrete SafetyValidator. Stateless.
type Validator struct{}

// New returns a Validator.
func New() *Validator { return &Validator{} }

func stripComments(sql string) string {
	sql = reBlockComment.ReplaceAllString(sql, " ")
	sql = reLineComment.ReplaceAllString(sql, " ")
	return strings.TrimSpace(sql)
}

// AssertSafeSelect returns the cleaned single-statement SELECT/WITH body, or
// wraps domain.ErrUnsafeSQL. This is the only function allowed to decide
// whether SQL reaches the DB.
func (Validator) AssertSafeSelect(sql string) (string, error) {
	cleaned := stripComments(sql)
	if cleaned == "" {
		return "", fmt.Errorf("%w: empty query", domain.ErrUnsafeSQL)
	}

	var statements []string
	for _, s := range strings.Split(cleaned, ";") {
		if strings.TrimSpace(s) != "" {
			statements = append(statements, s)
		}
	}
	if len(statements) > 1 {
		return "", fmt.Errorf("%w: found multiple statements", domain.ErrUnsafeSQL)
	}
	if len(statements) == 0 {
		return "", fmt.Errorf("%w: empty query", domain.ErrUnsafeSQL)
	}

	body := strings.TrimSpace(statements[0])
	first := strings.ToLower(strings.Fields(body)[0])
	if first != "select" && first != "with" {
		return "", fmt.Errorf("%w: got %q, want SELECT/WITH", domain.ErrUnsafeSQL, first)
	}
	if forbidden.MatchString(body) {
		return "", fmt.Errorf("%w: contains a forbidden write/DDL keyword", domain.ErrUnsafeSQL)
	}
	return body, nil
}
