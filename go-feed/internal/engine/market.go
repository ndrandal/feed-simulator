package engine

import (
	"math"
	"sync"

	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

const (
	baseDailyVol    = 0.02  // 2% daily volatility
	sectorBlend     = 0.60  // 60% sector shock, 40% idiosyncratic
	driftPerTick    = 0.0   // zero drift for simulation
	ticksPerDay     = 86400 // approximate, for vol scaling
)

// MarketEngine drives GBM price movement with sector-correlated returns.
type MarketEngine struct {
	mu     sync.RWMutex
	rng    *RNG
	prices map[uint16]float64   // locate -> current price
	syms   []symbol.Symbol
	byLoc  map[uint16]*symbol.Symbol

	// sector shocks generated once per tick cycle
	sectorShocks map[symbol.Sector]float64
}

// NewMarketEngine creates a price engine for all symbols.
func NewMarketEngine(rng *RNG, syms []symbol.Symbol) *MarketEngine {
	prices := make(map[uint16]float64, len(syms))
	byLoc := make(map[uint16]*symbol.Symbol, len(syms))
	for i := range syms {
		prices[syms[i].LocateCode] = syms[i].BasePrice
		byLoc[syms[i].LocateCode] = &syms[i]
	}
	return &MarketEngine{
		rng:          rng,
		prices:       prices,
		syms:         syms,
		byLoc:        byLoc,
		sectorShocks: make(map[symbol.Sector]float64),
	}
}

// GenerateSectorShocks produces one gaussian shock per sector.
// Call this once per tick cycle before ticking individual symbols.
func (m *MarketEngine) GenerateSectorShocks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sec := range symbol.Sectors() {
		m.sectorShocks[sec] = m.rng.Gaussian()
	}
}

// Tick advances the price for a single symbol and returns the new price.
// GBM: S(t+1) = S(t) * exp(drift + vol * Z)
func (m *MarketEngine) Tick(locateCode uint16) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	sym := m.byLoc[locateCode]
	if sym == nil {
		return 0
	}

	price := m.prices[locateCode]

	// Per-tick volatility: daily vol / sqrt(ticks_per_day) * symbol multiplier
	tickVol := baseDailyVol / math.Sqrt(ticksPerDay) * sym.VolatilityMultiplier

	// Blended shock: sector + idiosyncratic
	sectorZ := m.sectorShocks[sym.Sector]
	idioZ := m.rng.Gaussian()
	z := sectorBlend*sectorZ + (1-sectorBlend)*idioZ

	// GBM step
	logReturn := driftPerTick + tickVol*z
	price *= math.Exp(logReturn)

	// Snap to tick size, floor at 1 tick
	price = math.Round(price/sym.TickSize) * sym.TickSize
	if price < sym.TickSize {
		price = sym.TickSize
	}

	m.prices[locateCode] = price
	return price
}

// Price returns the current price for a symbol.
func (m *MarketEngine) Price(locateCode uint16) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prices[locateCode]
}

// SetPrice sets the price for a symbol (used when restoring from DB).
func (m *MarketEngine) SetPrice(locateCode uint16, price float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prices[locateCode] = price
}

// AllPrices returns a snapshot of all current prices.
func (m *MarketEngine) AllPrices() map[uint16]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[uint16]float64, len(m.prices))
	for k, v := range m.prices {
		out[k] = v
	}
	return out
}
