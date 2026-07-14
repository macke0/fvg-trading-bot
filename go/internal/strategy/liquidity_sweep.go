package strategy

import (
	"fvgbot/internal/indicators"
	"fvgbot/internal/types"
)

// LiquiditySweep trades stop-hunts around higher-timeframe (HTF) swing levels.
// Swing highs/lows are identified on a coarser timeframe built by aggregating
// the incoming lower-timeframe (LTF) bars; an entry triggers when one LTF bar
// wicks past an HTF level (sweeping the liquidity resting there) but closes back
// inside, and the next LTF bar confirms the reversal.
type LiquiditySweep struct {
	HTFFactor        int     // how many LTF bars make one HTF bar (e.g. 3 => 5m->15m)
	SwingLookback    int     // number of HTF bars to search for swing levels
	SwingMinAge      int     // skip the most recent N HTF bars (let levels establish)
	RiskReward       float64 // target distance as a multiple of the stop distance
	Buffer           float64 // padding beyond the sweep extreme for the stop
	EarlySessionOnly bool    // restrict entries to 09:30-12:00 ET
}

// NewLiquiditySweep returns a LiquiditySweep strategy with sensible defaults.
func NewLiquiditySweep() *LiquiditySweep {
	return &LiquiditySweep{
		HTFFactor:        3,  // 5-minute -> 15-minute
		SwingLookback:    20, // 20 HTF bars = 5 hours of 15-minute data
		SwingMinAge:      2,
		RiskReward:       2.0,
		Buffer:           0.01,
		EarlySessionOnly: true,
	}
}

func (s *LiquiditySweep) Name() string { return "Liquidity_Sweep_MTF" }

func (s *LiquiditySweep) OnBar(bar types.Bar, history []types.Bar) types.Signal {
	// Enough LTF history to build (SwingLookback + SwingMinAge) HTF bars, plus
	// the two most recent LTF bars (sweep + confirmation).
	minLTFBars := (s.SwingLookback+s.SwingMinAge)*s.HTFFactor + 2
	if len(history) < minLTFBars {
		return types.Signal{Action: types.Hold, Reason: "insufficient data"}
	}
	if s.EarlySessionOnly && !inEarlySession(bar.Timestamp) {
		return types.Signal{Action: types.Hold, Reason: "outside early session"}
	}

	current := history[len(history)-1] // LTF confirmation bar
	prev := history[len(history)-2]    // LTF potential sweep bar

	// Build HTF bars from all LTF bars EXCEPT the last two, so the levels are
	// established history and not influenced by the ongoing sweep/confirmation.
	htf := indicators.AggregateBars(history[:len(history)-2], s.HTFFactor)
	if len(htf) < s.SwingLookback+s.SwingMinAge {
		return types.Signal{Action: types.Hold, Reason: "insufficient HTF data"}
	}

	// Ignore the newest SwingMinAge HTF bars when reading swing levels.
	prior := htf[:len(htf)-s.SwingMinAge]
	swingHigh := indicators.SwingHigh(prior, s.SwingLookback)
	swingLow := indicators.SwingLow(prior, s.SwingLookback)

	// Sweep above an HTF swing high: prev bar wicked over it but closed back
	// under, and the current bar confirms bearishly -> go short.
	if prev.High > swingHigh && prev.Close < swingHigh && current.Close < current.Open {
		stop := prev.High + s.Buffer
		risk := stop - current.Close
		return types.Signal{
			Action:   types.Sell,
			Size:     1.0,
			StopLoss: stop,
			Target:   current.Close - risk*s.RiskReward,
			Reason:   "liquidity sweep above HTF swing high, bearish confirmation",
		}
	}

	// Sweep below an HTF swing low: prev bar wicked under it but closed back
	// above, and the current bar confirms bullishly -> go long.
	if prev.Low < swingLow && prev.Close > swingLow && current.Close > current.Open {
		stop := prev.Low - s.Buffer
		risk := current.Close - stop
		return types.Signal{
			Action:   types.Buy,
			Size:     1.0,
			StopLoss: stop,
			Target:   current.Close + risk*s.RiskReward,
			Reason:   "liquidity sweep below HTF swing low, bullish confirmation",
		}
	}

	return types.Signal{Action: types.Hold, Reason: "no sweep detected"}
}
