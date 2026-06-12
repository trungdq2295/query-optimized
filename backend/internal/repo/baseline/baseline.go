// Package baseline is an in-memory implementation of port.BaselineStore. It
// holds "before" snapshots for the lifetime of the process, keyed by a short
// id. For a single-process desktop app or a stateless hosted instance this is
// enough; swap for a durable store (Redis/SQLite) by changing one line in deps.
package baseline

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/trudoan/query-optimizer/internal/domain"
)

// Store is a concurrency-safe in-memory baseline store.
type Store struct {
	mu sync.RWMutex
	m  map[string]domain.Baseline
}

// New returns an empty store.
func New() *Store {
	return &Store{m: make(map[string]domain.Baseline)}
}

// Save assigns an id (if absent), stamps CapturedAt, and stores the baseline.
func (s *Store) Save(b domain.Baseline) (string, error) {
	if b.ID == "" {
		b.ID = newID()
	}
	if b.CapturedAt.IsZero() {
		b.CapturedAt = time.Now()
	}
	s.mu.Lock()
	s.m[b.ID] = b
	s.mu.Unlock()
	return b.ID, nil
}

// Get returns the baseline for id and whether it was found.
func (s *Store) Get(id string) (domain.Baseline, bool) {
	s.mu.RLock()
	b, ok := s.m[id]
	s.mu.RUnlock()
	return b, ok
}

func newID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
