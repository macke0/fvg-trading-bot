// Command benchmark compares a strategy against simply buying and holding each
// symbol over the same period. It reports both raw return and — more importantly
// — return per unit of max drawdown, because buy-and-hold can post a big return
// while subjecting you to a huge peak-to-trough loss (e.g. tech in 2022).
//
// Returns are expressed as a percent of the capital needed to trade one share
// (the average price), so the strategy's price-point P/L is comparable to the
// buy-and-hold percentage return.
//
// Usage:
//
//	go run ./cmd/benchmark --strategy sma --vol-threshold 0 --factor 1 --cost 0.02
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/indicators"
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	stratName := flag.String("strategy", "sma", "strategy: sma or liq")
	factor := flag.Int("factor", 1, "timeframe aggregation on the 5-min base (1=5min, 3=15min, ...)")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")
	borrow := flag.Float64("borrow-rate", 0, "annualized short-borrow fee (e.g. 0.005 = 0.5%/yr); charged on shorts by holding time")

	smaPeriod := flag.Int("sma-period", 100, "SMA period in bars")
	volThreshold := flag.Float64("vol-threshold", 1.2, "volume filter multiple; 0 disables it")
	rr := flag.Float64("rr", 2.0, "risk-reward (liq)")
	htf := flag.Int("htf", 3, "HTF factor (liq)")
	lookback := flag.Int("lookback", 20, "swing lookback (liq)")
	session := flag.Bool("session", true, "restrict entries to 09:30-12:00 ET")
	flag.Parse()

	symbols := splitSymbols(*symbolsCSV)

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

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Printf("\n%s vs buy-and-hold  (%dmin, cost %.2f/side)\n\n", build().Name(), *factor*5, *cost)
	fmt.Fprintln(w, "symbol\tavg $\tB&H ret%\tB&H maxDD%\tB&H ret/DD\tstrat ret%\tstrat maxDD%\tstrat ret/DD\ttrades")

	var sumBHPnL, sumStratPnL, sumAvgPrice, sumFirst float64
	for _, sym := range symbols {
		bars, err := data.LoadCSV(filepath.Join(*dataDir, sym+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", sym, err)
			continue
		}
		bars = indicators.AggregateByDay(bars, *factor)
		if len(bars) < 2 {
			continue
		}

		first := bars[0].Close
		last := bars[len(bars)-1].Close
		avg := avgClose(bars)

		bhRet := (last - first) / first * 100
		bhDD := buyHoldMaxDrawdownPct(bars)

		var stratSum engine.Summary
		cfg := engine.Config{CostPerTrade: *cost, BorrowRateAnnual: *borrow}
		if s := engine.Summarize(engine.Run(build(), sym, bars, cfg)); len(s) > 0 {
			stratSum = s[0]
		}
		stratRet := stratSum.TotalPnL / avg * 100
		stratDD := stratSum.MaxDrawdown / avg * 100

		fmt.Fprintf(w, "%s\t%.2f\t%+.1f\t%.1f\t%s\t%+.1f\t%.1f\t%s\t%d\n",
			sym, avg, bhRet, bhDD, ratio(bhRet, bhDD), stratRet, stratDD, ratio(stratRet, stratDD), stratSum.Trades)

		sumBHPnL += last - first
		sumStratPnL += stratSum.TotalPnL
		sumAvgPrice += avg
		sumFirst += first
	}
	w.Flush()

	if sumFirst > 0 {
		fmt.Printf("\nPortfolio (1 share of each):\n")
		fmt.Printf("  buy-and-hold: %+.2f  (%+.1f%% of first-day cost)\n", sumBHPnL, sumBHPnL/sumFirst*100)
		fmt.Printf("  strategy:     %+.2f  (%+.1f%% of avg capital)\n", sumStratPnL, sumStratPnL/sumAvgPrice*100)
	}

	fmt.Println("\nret/DD is return divided by max drawdown — higher is better risk-adjusted.")
	fmt.Println("A strategy that earns less than buy-and-hold can still win here if it did so")
	fmt.Println("with a much smaller drawdown (it sidestepped the crash).")
}

func avgClose(bars []types.Bar) float64 {
	sum := 0.0
	for _, b := range bars {
		sum += b.Close
	}
	return sum / float64(len(bars))
}

// buyHoldMaxDrawdownPct is the worst peak-to-trough drop of the close price, %.
func buyHoldMaxDrawdownPct(bars []types.Bar) float64 {
	peak := bars[0].Close
	maxDD := 0.0
	for _, b := range bars {
		if b.Close > peak {
			peak = b.Close
		}
		if peak > 0 {
			if dd := (peak - b.Close) / peak * 100; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func ratio(ret, dd float64) string {
	if dd <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", ret/dd)
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
