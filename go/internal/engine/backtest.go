// Package engine runs a strategy over historical bars and records the trades.
package engine

import (
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

// TradeRecord is one row of backtest output. An entry row uses Action BUY/SELL
// with ProfitLoss 0; the matching exit row uses an Action starting with "CLOSE"
// and carries the realized ProfitLoss.
type TradeRecord struct {
	Strategy   string
	Symbol     string
	Timestamp  int64
	Action     string
	Price      float64
	ProfitLoss float64
}

// Config controls execution assumptions applied on top of the raw strategy.
type Config struct {
	// CostPerTrade is a flat cost (commission + slippage, in price units)
	// charged once on entry and once on exit. Zero models a frictionless market.
	CostPerTrade float64
}

type position struct {
	open     bool
	isLong   bool
	entry    float64
	stopLoss float64
	target   float64
	size     float64
}

// Run walks bars in order, feeding each to the strategy and simulating a single
// open position at a time.
//
//   - Entries fill at the CLOSE of the signal bar.
//   - Stop/target are checked only on SUBSEQUENT bars, using each bar's
//     high/low, so there is no look-ahead inside the entry bar.
//   - If one bar's range spans both stop and target, the stop is taken first
//     (conservative / worst-case).
//   - A position still open at the end is closed at the last bar's close.
func Run(s strategy.Strategy, symbol string, bars []types.Bar, cfg Config) []TradeRecord {
	var records []TradeRecord
	var pos position

	for i := range bars {
		bar := bars[i]

		if pos.open {
			if price, action, hit := checkExit(pos, bar); hit {
				records = append(records, TradeRecord{
					Strategy:   s.Name(),
					Symbol:     symbol,
					Timestamp:  bar.Timestamp,
					Action:     action,
					Price:      price,
					ProfitLoss: pnl(pos, price, cfg.CostPerTrade),
				})
				pos = position{}
			}
			// At most one action per bar; don't re-enter on an exit bar.
			continue
		}

		sig := s.OnBar(bar, bars[:i+1])
		if sig.Action != types.Buy && sig.Action != types.Sell {
			continue
		}

		size := sig.Size
		if size <= 0 {
			size = 1.0
		}
		pos = position{
			open:     true,
			isLong:   sig.Action == types.Buy,
			entry:    bar.Close,
			stopLoss: sig.StopLoss,
			target:   sig.Target,
			size:     size,
		}
		records = append(records, TradeRecord{
			Strategy:  s.Name(),
			Symbol:    symbol,
			Timestamp: bar.Timestamp,
			Action:    string(sig.Action),
			Price:     bar.Close,
		})
	}

	if pos.open && len(bars) > 0 {
		last := bars[len(bars)-1]
		records = append(records, TradeRecord{
			Strategy:   s.Name(),
			Symbol:     symbol,
			Timestamp:  last.Timestamp,
			Action:     "CLOSE_EOD",
			Price:      last.Close,
			ProfitLoss: pnl(pos, last.Close, cfg.CostPerTrade),
		})
	}

	return records
}

// checkExit reports whether bar triggers the position's stop or target. Stop is
// checked first so an ambiguous bar counts as a loss.
func checkExit(pos position, bar types.Bar) (price float64, action string, hit bool) {
	if pos.isLong {
		if bar.Low <= pos.stopLoss {
			return pos.stopLoss, "CLOSE_STOP", true
		}
		if bar.High >= pos.target {
			return pos.target, "CLOSE_TARGET", true
		}
		return 0, "", false
	}
	if bar.High >= pos.stopLoss {
		return pos.stopLoss, "CLOSE_STOP", true
	}
	if bar.Low <= pos.target {
		return pos.target, "CLOSE_TARGET", true
	}
	return 0, "", false
}

// pnl is the realized profit/loss of closing pos at exitPrice, net of the round
// trip's entry+exit costs.
func pnl(pos position, exitPrice, costPerTrade float64) float64 {
	var gross float64
	if pos.isLong {
		gross = (exitPrice - pos.entry) * pos.size
	} else {
		gross = (pos.entry - exitPrice) * pos.size
	}
	return gross - 2*costPerTrade*pos.size
}
