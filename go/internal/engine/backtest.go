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

// secondsPerYear is used to prorate the annualized borrow rate over a position's
// holding time.
const secondsPerYear = 365 * 24 * 60 * 60

// Config controls execution assumptions applied on top of the raw strategy.
type Config struct {
	// CostPerTrade is a flat cost (commission + slippage, in price units)
	// charged once on entry and once on exit. Zero models a frictionless market.
	CostPerTrade float64

	// BorrowRateAnnual is the annualized stock-borrow fee charged on SHORT
	// positions only, applied to the position's notional (entry price x size)
	// prorated by holding time. e.g. 0.005 = 0.5%/yr (typical liquid large cap);
	// hard-to-borrow names can be much higher. Zero models free shorting.
	BorrowRateAnnual float64
}

type position struct {
	open      bool
	isLong    bool
	entry     float64
	stopLoss  float64
	target    float64
	size      float64
	entryTime int64
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
					ProfitLoss: pnl(pos, price, bar.Timestamp, cfg),
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
			open:      true,
			isLong:    sig.Action == types.Buy,
			entry:     bar.Close,
			stopLoss:  sig.StopLoss,
			target:    sig.Target,
			size:      size,
			entryTime: bar.Timestamp,
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
			ProfitLoss: pnl(pos, last.Close, last.Timestamp, cfg),
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

// pnl is the realized profit/loss of closing pos at exitPrice/exitTime, net of
// the round trip's entry+exit costs and, for shorts, the prorated borrow fee.
func pnl(pos position, exitPrice float64, exitTime int64, cfg Config) float64 {
	var gross float64
	if pos.isLong {
		gross = (exitPrice - pos.entry) * pos.size
	} else {
		gross = (pos.entry - exitPrice) * pos.size
	}
	net := gross - 2*cfg.CostPerTrade*pos.size

	// Borrow fee applies to shorts only, on notional, prorated by holding time.
	if !pos.isLong && cfg.BorrowRateAnnual > 0 {
		held := exitTime - pos.entryTime
		if held > 0 {
			notional := pos.entry * pos.size
			net -= notional * cfg.BorrowRateAnnual * (float64(held) / secondsPerYear)
		}
	}
	return net
}
