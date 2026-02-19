package engine

import (
	"math"
	"testing"

	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

func newTestMarket() (*MarketEngine, *RNG) {
	rng := NewRNG(42)
	syms := symbol.AllSymbols()
	return NewMarketEngine(rng, syms), rng
}

func TestInitialPrices(t *testing.T) {
	m, _ := newTestMarket()
	for _, s := range symbol.AllSymbols() {
		got := m.Price(s.LocateCode)
		if got != s.BasePrice {
			t.Errorf("%s: initial price = %f, want %f", s.Ticker, got, s.BasePrice)
		}
	}
}

func TestPricePositivityOver100kTicks(t *testing.T) {
	m, _ := newTestMarket()
	syms := symbol.AllSymbols()
	for i := 0; i < 100000; i++ {
		m.GenerateSectorShocks()
		for _, s := range syms {
			p := m.Tick(s.LocateCode)
			if p <= 0 {
				t.Fatalf("%s: price went non-positive at tick %d: %f", s.Ticker, i, p)
			}
		}
	}
}

func TestTickSizeSnapping(t *testing.T) {
	m, _ := newTestMarket()
	syms := symbol.AllSymbols()
	for i := 0; i < 1000; i++ {
		m.GenerateSectorShocks()
		for _, s := range syms {
			p := m.Tick(s.LocateCode)
			// Price should be a multiple of tick size (0.01)
			remainder := math.Mod(p, s.TickSize)
			// Account for floating-point imprecision
			if remainder > 0.001 && remainder < s.TickSize-0.001 {
				t.Fatalf("%s: price %f not snapped to tick size %f (remainder %f)", s.Ticker, p, s.TickSize, remainder)
			}
		}
	}
}

func TestSameSectorCorrelation(t *testing.T) {
	// Run many ticks and measure correlation between same-sector vs cross-sector
	rng := NewRNG(42)
	syms := symbol.AllSymbols()
	m := NewMarketEngine(rng, syms)

	// Find two Tech symbols and one Finance symbol
	var tech1, tech2, fin1 *symbol.Symbol
	for i := range syms {
		switch {
		case syms[i].Sector == symbol.SectorTech && tech1 == nil:
			tech1 = &syms[i]
		case syms[i].Sector == symbol.SectorTech && tech2 == nil:
			tech2 = &syms[i]
		case syms[i].Sector == symbol.SectorFinance && fin1 == nil:
			fin1 = &syms[i]
		}
	}

	n := 10000
	sameSectorCorr := 0.0
	crossSectorCorr := 0.0

	prevTech1 := m.Price(tech1.LocateCode)
	prevTech2 := m.Price(tech2.LocateCode)
	prevFin1 := m.Price(fin1.LocateCode)

	for i := 0; i < n; i++ {
		m.GenerateSectorShocks()
		p1 := m.Tick(tech1.LocateCode)
		p2 := m.Tick(tech2.LocateCode)
		p3 := m.Tick(fin1.LocateCode)

		r1 := (p1 - prevTech1) / prevTech1
		r2 := (p2 - prevTech2) / prevTech2
		r3 := (p3 - prevFin1) / prevFin1

		sameSectorCorr += r1 * r2
		crossSectorCorr += r1 * r3

		prevTech1, prevTech2, prevFin1 = p1, p2, p3
	}

	sameSectorCorr /= float64(n)
	crossSectorCorr /= float64(n)

	// Same-sector should have higher correlation
	if sameSectorCorr <= crossSectorCorr {
		t.Errorf("same-sector corr (%e) should exceed cross-sector corr (%e)", sameSectorCorr, crossSectorCorr)
	}
}

func TestSetPrice(t *testing.T) {
	m, _ := newTestMarket()
	m.SetPrice(1, 999.99)
	if got := m.Price(1); got != 999.99 {
		t.Fatalf("SetPrice: got %f, want 999.99", got)
	}
}

func TestAllPricesSnapshot(t *testing.T) {
	m, _ := newTestMarket()
	prices := m.AllPrices()
	if len(prices) != 30 {
		t.Fatalf("AllPrices returned %d entries, want 30", len(prices))
	}
	// Mutating the snapshot should not affect the engine
	for k := range prices {
		prices[k] = 0
	}
	if m.Price(1) == 0 {
		t.Fatal("AllPrices snapshot mutation affected the engine")
	}
}

func TestTickUnknownLocate(t *testing.T) {
	m, _ := newTestMarket()
	m.GenerateSectorShocks()
	p := m.Tick(999)
	if p != 0 {
		t.Fatalf("Tick with unknown locate should return 0, got %f", p)
	}
}

func TestPriceUnknownLocate(t *testing.T) {
	m, _ := newTestMarket()
	p := m.Price(999)
	if p != 0 {
		t.Fatalf("Price with unknown locate should return 0, got %f", p)
	}
}

func TestTickReturnsSameAsPrice(t *testing.T) {
	m, _ := newTestMarket()
	m.GenerateSectorShocks()
	tickResult := m.Tick(1)
	priceResult := m.Price(1)
	if tickResult != priceResult {
		t.Fatalf("Tick returned %f but Price returned %f", tickResult, priceResult)
	}
}
