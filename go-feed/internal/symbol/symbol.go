package symbol

// Sector represents a market sector.
type Sector string

const (
	SectorTech       Sector = "Tech"
	SectorFinance    Sector = "Finance"
	SectorHealthcare Sector = "Healthcare"
	SectorEnergy     Sector = "Energy"
	SectorConsumer   Sector = "Consumer"
	SectorIndustrial Sector = "Industrial"
	SectorStress     Sector = "Stress"
	SectorETF        Sector = "ETF"
)

// Symbol holds metadata for a simulated trading instrument.
type Symbol struct {
	LocateCode          uint16
	Ticker              string
	Name                string
	Sector              Sector
	BasePrice           float64
	TickSize            float64
	VolatilityMultiplier float64
	IsStress            bool
}

// AllSymbols returns the 30 fake symbols across 7 sectors + ETFs.
func AllSymbols() []Symbol {
	return []Symbol{
		// Tech (6) — mid-high volatility
		{1, "NEXO", "Nexo Dynamics Inc", SectorTech, 185.00, 0.01, 1.4, false},
		{2, "QBIT", "Qbit Quantum Corp", SectorTech, 92.50, 0.01, 1.6, false},
		{3, "FLUX", "Flux Systems Ltd", SectorTech, 310.00, 0.01, 1.3, false},
		{4, "SYNK", "Synk Networks Inc", SectorTech, 67.25, 0.01, 1.5, false},
		{5, "PULS", "Puls Digital Corp", SectorTech, 145.00, 0.01, 1.2, false},
		{6, "CYRA", "Cyra Robotics Inc", SectorTech, 220.00, 0.01, 1.7, false},

		// Finance (5) — low-mid volatility
		{7, "LEDG", "Ledger Capital Group", SectorFinance, 78.50, 0.01, 0.8, false},
		{8, "VALT", "Vault Securities Inc", SectorFinance, 125.00, 0.01, 0.7, false},
		{9, "CRDT", "Credt Financial Corp", SectorFinance, 52.00, 0.01, 0.9, false},
		{10, "MNTX", "Mintex Banking Corp", SectorFinance, 165.00, 0.01, 0.6, false},
		{11, "FNDX", "Fundex Asset Mgmt", SectorFinance, 88.75, 0.01, 0.8, false},

		// Healthcare (4) — low volatility
		{12, "HELX", "Helix Biomedical Inc", SectorHealthcare, 195.00, 0.01, 0.5, false},
		{13, "CURA", "Cura Therapeutics", SectorHealthcare, 72.00, 0.01, 0.6, false},
		{14, "GENX", "GenX Genomics Corp", SectorHealthcare, 148.50, 0.01, 0.7, false},
		{15, "BIOS", "Bios Pharma Ltd", SectorHealthcare, 55.25, 0.01, 0.5, false},

		// Energy (4) — mid volatility
		{16, "VOLT", "Volt Energy Corp", SectorEnergy, 98.00, 0.01, 1.1, false},
		{17, "SOLR", "Solaris Power Inc", SectorEnergy, 42.50, 0.01, 1.0, false},
		{18, "FUSE", "Fuse Petroleum Ltd", SectorEnergy, 175.00, 0.01, 1.2, false},
		{19, "WATT", "Watt Grid Systems", SectorEnergy, 63.00, 0.01, 1.0, false},

		// Consumer (4) — low-mid volatility
		{20, "BRND", "Brand Global Inc", SectorConsumer, 112.00, 0.01, 0.8, false},
		{21, "LUXE", "Luxe Retail Corp", SectorConsumer, 285.00, 0.01, 0.7, false},
		{22, "DLVR", "Deliver Express Inc", SectorConsumer, 78.00, 0.01, 0.9, false},
		{23, "RSTK", "Restock Supply Corp", SectorConsumer, 45.50, 0.01, 0.8, false},

		// Industrial (4) — mid volatility
		{24, "FORG", "Forge Manufacturing", SectorIndustrial, 132.00, 0.01, 1.0, false},
		{25, "BLDR", "Builder Heavy Ind", SectorIndustrial, 88.00, 0.01, 1.1, false},
		{26, "MACH", "Mach Precision Corp", SectorIndustrial, 205.00, 0.01, 1.0, false},
		{27, "ALOY", "Aloy Materials Inc", SectorIndustrial, 56.75, 0.01, 1.2, false},

		// Stress (1) — always hot
		{28, "BLITZ", "Blitz Trading Corp", SectorStress, 125.00, 0.01, 2.0, true},

		// ETFs (2) — low volatility
		{29, "MKTS", "Markets Broad ETF", SectorETF, 350.00, 0.01, 0.4, false},
		{30, "GRWT", "Growth Select ETF", SectorETF, 180.00, 0.01, 0.5, false},
	}
}

// ByTicker returns a map from ticker to symbol for quick lookups.
func ByTicker() map[string]*Symbol {
	syms := AllSymbols()
	m := make(map[string]*Symbol, len(syms))
	for i := range syms {
		m[syms[i].Ticker] = &syms[i]
	}
	return m
}

// ByLocate returns a map from locate code to symbol.
func ByLocate() map[uint16]*Symbol {
	syms := AllSymbols()
	m := make(map[uint16]*Symbol, len(syms))
	for i := range syms {
		m[syms[i].LocateCode] = &syms[i]
	}
	return m
}

// Sectors returns unique sectors in order.
func Sectors() []Sector {
	return []Sector{
		SectorTech, SectorFinance, SectorHealthcare,
		SectorEnergy, SectorConsumer, SectorIndustrial,
		SectorStress, SectorETF,
	}
}

// SymbolsBySector groups symbols by their sector.
func SymbolsBySector() map[Sector][]Symbol {
	syms := AllSymbols()
	m := make(map[Sector][]Symbol)
	for _, s := range syms {
		m[s.Sector] = append(m[s.Sector], s)
	}
	return m
}
