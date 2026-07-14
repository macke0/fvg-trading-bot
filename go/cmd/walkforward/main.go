// Command walkforward measures how consistent a strategy is across time.
//
// The strategy's parameters are fixed (hand-chosen defaults, not fit to this
// data), so the entire history is effectively out-of-sample. It runs the
// strategy continuously over the full series and buckets the resulting closed
// trades by calendar month, so you can see whether the edge shows up in most
// months (robust) or comes from a few lucky ones (fragile). It also checks
// whether the total survives removing the single best month.
//
// Usage:
//
//	go run ./cmd/walkforward --strategy sma --vol-threshold 0 --factor 3 --cost 0.02
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/indicators"
	"fvgbot/internal/strategy"
)

// bucket accumulates one month's closed-trade performance.
type bucket struct {
	month  string
	trades int
	wins   int
	pnl    float64
}

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	stratName := flag.String("strategy", "sma", "strategy: sma or liq")
	factor := flag.Int("factor", 1, "timeframe aggregation on the 5-min base (1=5min, 3=15min, 6=30min, 12=60min)")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")

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

	// Continuous run over the full series per symbol; collect closed trades.
	byMonth := map[string]*bucket{}
	var months []string
	for _, sym := range symbols {
		bars, err := data.LoadCSV(filepath.Join(*dataDir, sym+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", sym, err)
			continue
		}
		bars = indicators.AggregateByDay(bars, *factor)

		for _, r := range engine.Run(build(), sym, bars, engine.Config{CostPerTrade: *cost}) {
			if !strings.HasPrefix(r.Action, "CLOSE") {
				continue
			}
			m := time.Unix(r.Timestamp, 0).UTC().Format("2006-01")
			b, ok := byMonth[m]
			if !ok {
				b = &bucket{month: m}
				byMonth[m] = b
				months = append(months, m)
			}
			b.trades++
			b.pnl += r.ProfitLoss
			if r.ProfitLoss > 0 {
				b.wins++
			}
		}
	}
	if len(months) == 0 {
		log.Fatal("no closed trades produced")
	}
	sort.Strings(months)

	printReport(build().Name(), *factor, *cost, months, byMonth)
}

func printReport(name string, factor int, cost float64, months []string, byMonth map[string]*bucket) {
	fmt.Printf("\nWalk-forward: %s, continuous run bucketed by month\n", name)
	fmt.Printf("timeframe %dmin   cost %.2f/side   %d months\n\n", factor*5, cost, len(months))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "month\ttrades\twin%\tP/L\tcumulative")

	var cum, total float64
	var profitable int
	var best, worst *bucket
	for _, m := range months {
		b := byMonth[m]
		cum += b.pnl
		total += b.pnl
		if b.pnl > 0 {
			profitable++
		}
		if best == nil || b.pnl > best.pnl {
			best = b
		}
		if worst == nil || b.pnl < worst.pnl {
			worst = b
		}
		win := 0.0
		if b.trades > 0 {
			win = float64(b.wins) / float64(b.trades) * 100
		}
		fmt.Fprintf(w, "%s\t%d\t%.1f\t%+.2f\t%+.2f\n", b.month, b.trades, win, b.pnl, cum)
	}
	w.Flush()

	n := len(months)
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  total P/L:            %+.2f\n", total)
	fmt.Printf("  profitable months:   %d / %d (%.0f%%)\n", profitable, n, float64(profitable)/float64(n)*100)
	fmt.Printf("  mean month:          %+.2f\n", total/float64(n))
	fmt.Printf("  best / worst month:  %+.2f (%s) / %+.2f (%s)\n", best.pnl, best.month, worst.pnl, worst.month)
	fmt.Printf("  total excl. best mo: %+.2f  %s\n", total-best.pnl, fragility(total-best.pnl))
	fmt.Println("\n  A robust edge shows up in most months and survives removing its best month.")
	fmt.Println("  If it's negative without one lucky month, it's fragile — treat with suspicion.")
}

func fragility(x float64) string {
	if x > 0 {
		return "(still positive — good sign)"
	}
	return "(goes NEGATIVE — one month carried it)"
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
