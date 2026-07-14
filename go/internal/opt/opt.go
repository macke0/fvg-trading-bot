// Package opt holds the parameter grid and evaluation logic shared by the
// sweep and out-of-sample commands, so both search the same space the same way.
package opt

import (
	"fvgbot/internal/engine"
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

// Params is one liquidity-sweep parameter setting.
type Params struct {
	HTFFactor     int
	SwingLookback int
	RiskReward    float64
}

// Default mirrors strategy.NewLiquiditySweep's defaults.
var Default = Params{HTFFactor: 3, SwingLookback: 20, RiskReward: 2.0}

func (p Params) build() *strategy.LiquiditySweep {
	s := strategy.NewLiquiditySweep()
	s.HTFFactor = p.HTFFactor
	s.SwingLookback = p.SwingLookback
	s.RiskReward = p.RiskReward
	return s
}

var (
	htfGrid      = []int{2, 3, 4, 6}
	lookbackGrid = []int{10, 15, 20, 30, 40}
	rrGrid       = []float64{1.0, 1.5, 2.0, 2.5, 3.0}
)

// Grid returns every parameter combination to evaluate.
func Grid() []Params {
	out := make([]Params, 0, len(htfGrid)*len(lookbackGrid)*len(rrGrid))
	for _, h := range htfGrid {
		for _, l := range lookbackGrid {
			for _, r := range rrGrid {
				out = append(out, Params{HTFFactor: h, SwingLookback: l, RiskReward: r})
			}
		}
	}
	return out
}

// Evaluate runs one parameter set across all symbols and returns the combined
// closed-trade summary.
func Evaluate(p Params, symbols []string, bars map[string][]types.Bar, cost float64) engine.Summary {
	s := p.build()
	var recs []engine.TradeRecord
	for _, sym := range symbols {
		b, ok := bars[sym]
		if !ok || len(b) == 0 {
			continue
		}
		recs = append(recs, engine.Run(s, sym, b, engine.Config{CostPerTrade: cost})...)
	}
	if sums := engine.Summarize(recs); len(sums) > 0 {
		return sums[0]
	}
	return engine.Summary{Strategy: s.Name()}
}
