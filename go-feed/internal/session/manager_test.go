package session

import (
	"testing"

	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

func newTestManager() *Manager {
	return NewManager(symbol.AllSymbols(), 100)
}

func TestResolveTickersSpecific(t *testing.T) {
	m := newTestManager()
	locs, all := m.ResolveTickers([]string{"NEXO", "QBIT"})
	if all {
		t.Fatal("should not be all")
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locates, got %d", len(locs))
	}
	locSet := make(map[uint16]bool)
	for _, l := range locs {
		locSet[l] = true
	}
	if !locSet[1] || !locSet[2] {
		t.Fatalf("expected locates 1 and 2, got %v", locs)
	}
}

func TestResolveTickersWildcard(t *testing.T) {
	m := newTestManager()
	locs, all := m.ResolveTickers([]string{"*"})
	if !all {
		t.Fatal("wildcard should set all=true")
	}
	if locs != nil {
		t.Fatalf("wildcard should return nil locates, got %v", locs)
	}
}

func TestResolveTickersUnknown(t *testing.T) {
	m := newTestManager()
	locs, all := m.ResolveTickers([]string{"ZZZZ"})
	if all {
		t.Fatal("should not be all")
	}
	if len(locs) != 0 {
		t.Fatalf("expected 0 locates for unknown ticker, got %d", len(locs))
	}
}

func TestResolveTickersMixed(t *testing.T) {
	m := newTestManager()
	locs, all := m.ResolveTickers([]string{"NEXO", "ZZZZ", "BLITZ"})
	if all {
		t.Fatal("should not be all")
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locates (NEXO + BLITZ), got %d", len(locs))
	}
}

func TestResolveTickersWildcardShortCircuits(t *testing.T) {
	m := newTestManager()
	// Wildcard should return immediately even with other tickers
	locs, all := m.ResolveTickers([]string{"NEXO", "*", "BLITZ"})
	if !all {
		t.Fatal("wildcard should short-circuit to all=true")
	}
	if locs != nil {
		t.Fatalf("wildcard should return nil locates, got %v", locs)
	}
}
