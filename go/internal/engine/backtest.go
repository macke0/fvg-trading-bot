// Package engine runs a strategy over historical bars and records the trades.
package engine

import (
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

// secondsPerYear is used to prorate the annualized borrow rate over a position's
// holding time.
const secondsPerYear = 365 * 24 * 60 * 60

// TradeRecord is one row of backtest output. An entry row uses Action BUY/SELL
// with ProfitLoss 0; the matching exit row uses an Action starting with "CLOSE"
// and carries the realized ProfitLoss (per share, net of costs).
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

	// BorrowRateAnnual is the annualized stock-borrow fee charged on SHORT
	// positions only, applied to the position's notional (entry price x size)
	// prorated by holding time. e.g. 0.005 = 0.5%/yr (typical liquid large cap);
	// hard-to-borrow names can be much higher. Zero models free shorting.
	BorrowRateAnnual float64
}

// ClosedTrade is a completed round trip with everything needed for position
// sizing and equity accounting. Prices and P/L are per share.
type ClosedTrade struct {
	Symbol       string
	Long         bool
	EntryTime    int64
	ExitTime     int64
	Entry        float64
	Stop         float64
	Exit         float64
	ExitReason   string  // "CLOSE_STOP" / "CLOSE_TARGET" / "CLOSE_EOD"
	PnLPerShare  float64 // net of costs (and borrow, for shorts)
	RiskPerShare float64 // |entry - stop|, the loss-per-share if stopped out
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

// RunTrades walks bars in order and returns the completed trades. Fill/exit
// assumptions:
//
//   - Entries fill at the CLOSE of the signal bar.
//   - Stop/target are checked only on SUBSEQUENT bars, using each bar's
//     high/low, so there is no look-ahead inside the entry bar.
//   - If one bar's range spans both stop and target, the stop is taken first
//     (conservative / worst-case).
//   - A position still open at the end is closed at the last bar's close.
func RunTrades(s strategy.Strategy, symbol string, bars []types.Bar, cfg Config) []ClosedTrade {
	var trades []ClosedTrade
	var pos position

	for i := range bars {
		bar := bars[i]

		if pos.open {
			if price, reason, hit := checkExit(pos, bar); hit {
				trades = append(trades, closeTrade(pos, symbol, price, reason, bar.Timestamp, cfg))
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
	}

	if pos.open && len(bars) > 0 {
		last := bars[len(bars)-1]
		trades = append(trades, closeTrade(pos, symbol, last.Close, "CLOSE_EOD", last.Timestamp, cfg))
	}

	return trades
}

// Run is RunTrades expanded into entry+exit TradeRecords (the CSV / summary
// view). ProfitLoss on the exit row is per share, net of costs.
func Run(s strategy.Strategy, symbol string, bars []types.Bar, cfg Config) []TradeRecord {
	name := s.Name()
	var records []TradeRecord
	for _, t := range RunTrades(s, symbol, bars, cfg) {
		action := "BUY"
		if !t.Long {
			action = "SELL"
		}
		records = append(records,
			TradeRecord{Strategy: name, Symbol: symbol, Timestamp: t.EntryTime, Action: action, Price: t.Entry},
			TradeRecord{Strategy: name, Symbol: symbol, Timestamp: t.ExitTime, Action: t.ExitReason, Price: t.Exit, ProfitLoss: t.PnLPerShare},
		)
	}
	return records
}

// checkExit reports whether bar triggers the position's stop or target. Stop is
// checked first so an ambiguous bar counts as a loss.
func checkExit(pos position, bar types.Bar) (price float64, reason string, hit bool) {
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

// closeTrade builds a ClosedTrade from a position and its exit, computing the
// per-share P/L net of the round-trip costs and (for shorts) the borrow fee.
func closeTrade(pos position, symbol string, exitPrice float64, reason string, exitTime int64, cfg Config) ClosedTrade {
	risk := pos.entry - pos.stopLoss
	if risk < 0 {
		risk = -risk
	}

	var grossPS float64
	if pos.isLong {
		grossPS = exitPrice - pos.entry
	} else {
		grossPS = pos.entry - exitPrice
	}
	netPS := grossPS - 2*cfg.CostPerTrade
	if !pos.isLong && cfg.BorrowRateAnnual > 0 {
		if held := exitTime - pos.entryTime; held > 0 {
			netPS -= pos.entry * cfg.BorrowRateAnnual * (float64(held) / secondsPerYear)
		}
	}

	return ClosedTrade{
		Symbol:       symbol,
		Long:         pos.isLong,
		EntryTime:    pos.entryTime,
		ExitTime:     exitTime,
		Entry:        pos.entry,
		Stop:         pos.stopLoss,
		Exit:         exitPrice,
		ExitReason:   reason,
		PnLPerShare:  netPS,
		RiskPerShare: risk,
	}
}
