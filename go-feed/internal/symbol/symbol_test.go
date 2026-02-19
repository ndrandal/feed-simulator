package symbol

import "testing"

func TestAllSymbolsCount(t *testing.T) {
	syms := AllSymbols()
	if len(syms) != 30 {
		t.Fatalf("expected 30 symbols, got %d", len(syms))
	}
}

func TestLocateCodesUnique(t *testing.T) {
	syms := AllSymbols()
	seen := make(map[uint16]bool)
	for _, s := range syms {
		if seen[s.LocateCode] {
			t.Fatalf("duplicate locate code %d for %s", s.LocateCode, s.Ticker)
		}
		seen[s.LocateCode] = true
	}
}

func TestTickersUnique(t *testing.T) {
	syms := AllSymbols()
	seen := make(map[string]bool)
	for _, s := range syms {
		if seen[s.Ticker] {
			t.Fatalf("duplicate ticker %s", s.Ticker)
		}
		seen[s.Ticker] = true
	}
}

func TestLocateRange(t *testing.T) {
	for _, s := range AllSymbols() {
		if s.LocateCode < 1 || s.LocateCode > 30 {
			t.Fatalf("locate code %d out of range [1,30] for %s", s.LocateCode, s.Ticker)
		}
	}
}

func TestPositivePrices(t *testing.T) {
	for _, s := range AllSymbols() {
		if s.BasePrice <= 0 {
			t.Fatalf("non-positive base price %f for %s", s.BasePrice, s.Ticker)
		}
	}
}

func TestByTickerLookup(t *testing.T) {
	m := ByTicker()
	s, ok := m["NEXO"]
	if !ok {
		t.Fatal("NEXO not found in ByTicker")
	}
	if s.LocateCode != 1 {
		t.Fatalf("NEXO locate expected 1, got %d", s.LocateCode)
	}
}

func TestByTickerMissing(t *testing.T) {
	m := ByTicker()
	if _, ok := m["ZZZZ"]; ok {
		t.Fatal("expected ZZZZ to be missing")
	}
}

func TestByLocateLookup(t *testing.T) {
	m := ByLocate()
	s, ok := m[1]
	if !ok {
		t.Fatal("locate 1 not found in ByLocate")
	}
	if s.Ticker != "NEXO" {
		t.Fatalf("locate 1 expected NEXO, got %s", s.Ticker)
	}
}

func TestByLocateMissing(t *testing.T) {
	m := ByLocate()
	if _, ok := m[999]; ok {
		t.Fatal("expected locate 999 to be missing")
	}
}

func TestSectorsCount(t *testing.T) {
	secs := Sectors()
	if len(secs) != 8 {
		t.Fatalf("expected 8 sectors, got %d", len(secs))
	}
}

func TestSymbolsBySectorCounts(t *testing.T) {
	m := SymbolsBySector()
	expected := map[Sector]int{
		SectorTech:       6,
		SectorFinance:    5,
		SectorHealthcare: 4,
		SectorEnergy:     4,
		SectorConsumer:   4,
		SectorIndustrial: 4,
		SectorStress:     1,
		SectorETF:        2,
	}
	for sec, want := range expected {
		got := len(m[sec])
		if got != want {
			t.Errorf("sector %s: expected %d symbols, got %d", sec, want, got)
		}
	}
}

func TestBLITZIsStress(t *testing.T) {
	m := ByTicker()
	blitz, ok := m["BLITZ"]
	if !ok {
		t.Fatal("BLITZ not found")
	}
	if !blitz.IsStress {
		t.Fatal("BLITZ should have IsStress=true")
	}
	if blitz.Sector != SectorStress {
		t.Fatalf("BLITZ sector expected %s, got %s", SectorStress, blitz.Sector)
	}
}

func TestNonStressSymbols(t *testing.T) {
	for _, s := range AllSymbols() {
		if s.Ticker == "BLITZ" {
			continue
		}
		if s.IsStress {
			t.Fatalf("%s should not be marked as stress", s.Ticker)
		}
	}
}
