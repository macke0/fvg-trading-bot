// Command account simulates a real, compounding account trading all symbols
// together, with fixed-fractional position sizing (risk a fixed % of equity per
// trade) and a finite buying-power constraint. It reports the metrics you'd
// actually judge an account by: CAGR, max drawdown %, Sharpe, profit factor.
//
// Sizing: shares = (equity * risk) / (entry - stop). Each trade risks the same
// fraction of current equity, so a wider stop -> a smaller position. Positions
// across symbols run concurrently and share one pool of buying power
// (equity * max-leverage); trades that don't fit are skipped and counted.
//
// Usage:
//
//	go run ./cmd/account --strategy sma --vol-threshold 0 --factor 3 \
//	    --capital 10000 --risk 0.01 --cost 0.02 --borrow-rate 0.005
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/indicators"
	"fvgbot/internal/strategy"
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	stratName := flag.String("strategy", "sma", "strategy: sma or liq")
	factor := flag.Int("factor", 1, "timeframe aggregation on the 5-min base")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")
	borrow := flag.Float64("borrow-rate", 0, "annualized short-borrow fee (e.g. 0.005)")

	capital := flag.Float64("capital", 10000, "starting account equity")
	risk := flag.Float64("risk", 0.01, "fraction of equity to risk per trade (0.01 = 1%)")
	maxLev := flag.Float64("max-leverage", 1.0, "max total notional as a multiple of equity")

	smaPeriod := flag.Int("sma-period", 100, "SMA period in bars")
	volThreshold := flag.Float64("vol-threshold", 1.2, "volume filter multiple; 0 disables it")
	rr := flag.Float64("rr", 2.0, "risk-reward (liq)")
	htf := flag.Int("htf", 3, "HTF factor (liq)")
	lookback := flag.Int("lookback", 20, "swing lookback (liq)")
	session := flag.Bool("session", true, "restrict entries to 09:30-12:00 ET")
	flag.Parse()

	if *risk <= 0 || *risk >= 1 {
		log.Fatal("--risk must be between 0 and 1")
	}

	build := func() strategy.Strategy {
		switch strings.ToLower(*stratName) {
		case "sma":
			s := strategy.NewSMACross()
			s.SMAPeriod = *smaPeriod
			s.VolumeThreshold = *volThreshold
			s.EarlySessionOnly = *session
			return s
		case "liq":
			s := strategy.NewLiquiditySweep()
			s.RiskReward = *rr
			s.HTFFactor = *htf
			s.SwingLookback = *lookback
			s.EarlySessionOnly = *session
			return s
		default:
			log.Fatalf("unknown strategy %q (use sma or liq)", *stratName)
			return nil
		}
	}

	// Collect every completed trade across all symbols.
	cfg := engine.Config{CostPerTrade: *cost, BorrowRateAnnual: *borrow}
	var trades []engine.ClosedTrade
	for _, sym := range splitSymbols(*symbolsCSV) {
		bars, err := data.LoadCSV(filepath.Join(*dataDir, sym+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", sym, err)
			continue
		}
		bars = indicators.AggregateByDay(bars, *factor)
		trades = append(trades, engine.RunTrades(build(), sym, bars, cfg)...)
	}
	if len(trades) == 0 {
		log.Fatal("no trades produced")
	}

	res := simulate(trades, *capital, *risk, *maxLev)
	report(build().Name(), *factor, *capital, *risk, *maxLev, res)
}

// event is an entry or exit of a specific trade, used to drive the sim in time
// order.
type event struct {
	time   int64
	isExit bool
	idx    int
}

type openPos struct {
	shares   float64
	notional float64
}

type point struct {
	time   int64
	equity float64
}

type simResult struct {
	curve         []point
	final         float64
	taken         int
	skipped       int
	wins          int
	grossWin      float64
	grossLoss     float64
	firstTime     int64
	lastTime      int64
}

func simulate(trades []engine.ClosedTrade, capital, risk, maxLev float64) simResult {
	events := make([]event, 0, len(trades)*2)
	for i, t := range trades {
		events = append(events, event{time: t.EntryTime, isExit: false, idx: i})
		events = append(events, event{time: t.ExitTime, isExit: true, idx: i})
	}
	// Time order; on ties, process exits before entries (free buying power first).
	sort.SliceStable(events, func(a, b int) bool {
		if events[a].time != events[b].time {
			return events[a].time < events[b].time
		}
		return events[a].isExit && !events[b].isExit
	})

	equity := capital
	deployed := 0.0
	open := map[int]openPos{}

	res := simResult{final: capital, firstTime: events[0].time, lastTime: events[len(events)-1].time}
	res.curve = append(res.curve, point{time: events[0].time, equity: capital})

	for _, ev := range events {
		t := trades[ev.idx]
		if ev.isExit {
			p, ok := open[ev.idx]
			if !ok {
				continue // entry was skipped
			}
			pnl := p.shares * t.PnLPerShare
			equity += pnl
			deployed -= p.notional
			delete(open, ev.idx)
			if pnl > 0 {
				res.wins++
				res.grossWin += pnl
			} else {
				res.grossLoss += -pnl
			}
			res.curve = append(res.curve, point{time: ev.time, equity: equity})
			continue
		}

		// Entry: size by risk, capped by available buying power.
		if t.RiskPerShare <= 0 || equity <= 0 {
			res.skipped++
			continue
		}
		bp := equity*maxLev - deployed
		if bp <= 0 {
			res.skipped++
			continue
		}
		shares := (equity * risk) / t.RiskPerShare
		if notional := shares * t.Entry; notional > bp {
			shares = bp / t.Entry // cap to remaining buying power
		}
		if shares <= 0 {
			res.skipped++
			continue
		}
		open[ev.idx] = openPos{shares: shares, notional: shares * t.Entry}
		deployed += shares * t.Entry
		res.taken++
	}

	res.final = equity
	return res
}

func report(name string, factor int, capital, risk, maxLev float64, r simResult) {
	years := float64(r.lastTime-r.firstTime) / (365 * 24 * 60 * 60)
	totalRet := (r.final/capital - 1) * 100
	cagr := 0.0
	if years > 0 && r.final > 0 {
		cagr = (math.Pow(r.final/capital, 1/years) - 1) * 100
	}
	maxDD := maxDrawdownPct(r.curve)
	sharpe, annVol := sharpeRatio(r.curve, capital)

	winRate := 0.0
	if r.taken > 0 {
		winRate = float64(r.wins) / float64(r.taken) * 100
	}
	profitFactor := math.Inf(1)
	if r.grossLoss > 0 {
		profitFactor = r.grossWin / r.grossLoss
	}

	fmt.Printf("\nAccount simulation: %s  (%dmin)\n", name, factor*5)
	fmt.Printf("start $%.0f   risk %.1f%%/trade   max leverage %.1fx   %.2f years\n\n",
		capital, risk*100, maxLev, years)

	fmt.Printf("  final equity:      $%.0f\n", r.final)
	fmt.Printf("  total return:      %+.1f%%\n", totalRet)
	fmt.Printf("  CAGR:              %+.1f%%\n", cagr)
	fmt.Printf("  max drawdown:      %.1f%%\n", maxDD)
	fmt.Printf("  annualized vol:    %.1f%%\n", annVol)
	fmt.Printf("  Sharpe (approx):   %.2f\n", sharpe)
	fmt.Printf("  trades taken:      %d  (skipped %d, no buying power)\n", r.taken, r.skipped)
	fmt.Printf("  win rate:          %.1f%%\n", winRate)
	fmt.Printf("  profit factor:     %.2f\n", profitFactor)

	fmt.Println("\n  Equity is a closed-trade curve (updates when trades close); Sharpe uses")
	fmt.Println("  daily returns and idle days count as flat, so it is a rough figure.")
}

func maxDrawdownPct(curve []point) float64 {
	peak := curve[0].equity
	maxDD := 0.0
	for _, p := range curve {
		if p.equity > peak {
			peak = p.equity
		}
		if peak > 0 {
			if dd := (peak - p.equity) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// sharpeRatio builds a daily equity series (forward-filled) and returns the
// annualized Sharpe and annualized volatility (%).
func sharpeRatio(curve []point, capital float64) (sharpe, annVolPct float64) {
	if len(curve) < 2 {
		return 0, 0
	}
	const day = 24 * 60 * 60
	eqByDay := map[int64]float64{}
	for _, p := range curve {
		eqByDay[p.time/day] = p.equity // curve is time-sorted; last write wins
	}
	firstDay := curve[0].time / day
	lastDay := curve[len(curve)-1].time / day

	var returns []float64
	prev := capital
	for d := firstDay; d <= lastDay; d++ {
		eq, ok := eqByDay[d]
		if !ok {
			eq = prev
		}
		if prev > 0 {
			returns = append(returns, eq/prev-1)
		}
		prev = eq
	}
	if len(returns) < 2 {
		return 0, 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	varSum := 0.0
	for _, r := range returns {
		varSum += (r - mean) * (r - mean)
	}
	sd := math.Sqrt(varSum / float64(len(returns)-1))
	if sd == 0 {
		return 0, 0
	}
	return mean / sd * math.Sqrt(252), sd * math.Sqrt(252) * 100
}

func splitSymbols(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}
