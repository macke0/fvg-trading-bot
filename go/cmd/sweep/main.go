// Command sweep runs a grid of parameter combinations for the liquidity-sweep
// strategy and prints them ranked by net P/L, plus distribution statistics that
// make overfitting visible: if only a few combos are profitable and the median
// is a loss, a good-looking "best" result is likely luck, not edge.
//
// Usage:
//
//	go run ./cmd/sweep --symbols AAPL,MSFT,NVDA --cost 0.02 --top 15
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

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

// combo is one parameter setting and the performance it produced.
type combo struct {
	htf      int
	lookback int
	rr       float64
	trades   int
	wins     int
	pnl      float64
	maxDD    float64
}

func (c combo) winRate() float64 {
	if c.trades == 0 {
		return 0
	}
	return float64(c.wins) / float64(c.trades) * 100
}

// default parameters, so we can show where the "chosen" setting ranks.
const (
	defHTF      = 3
	defLookback = 20
	defRR       = 2.0
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")
	top := flag.Int("top", 15, "how many of the best combos to list")
	flag.Parse()

	symbols := splitSymbols(*symbolsCSV)
	bars := map[string][]types.Bar{}
	for _, s := range symbols {
		b, err := data.LoadCSV(filepath.Join(*dataDir, s+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", s, err)
			continue
		}
		bars[s] = b
	}
	if len(bars) == 0 {
		log.Fatal("no data loaded")
	}

	// Parameter grid. SwingMinAge is held at its default (2).
	htfGrid := []int{2, 3, 4, 6}
	lookbackGrid := []int{10, 15, 20, 30, 40}
	rrGrid := []float64{1.0, 1.5, 2.0, 2.5, 3.0}

	var results []combo
	for _, htf := range htfGrid {
		for _, lb := range lookbackGrid {
			for _, rr := range rrGrid {
				s := strategy.NewLiquiditySweep()
				s.HTFFactor = htf
				s.SwingLookback = lb
				s.RiskReward = rr

				var recs []engine.TradeRecord
				for _, sym := range symbols {
					b, ok := bars[sym]
					if !ok {
						continue
					}
					recs = append(recs, engine.Run(s, sym, b, engine.Config{CostPerTrade: *cost})...)
				}

				var sum engine.Summary
				if s := engine.Summarize(recs); len(s) > 0 {
					sum = s[0]
				}
				results = append(results, combo{
					htf: htf, lookback: lb, rr: rr,
					trades: sum.Trades, wins: sum.Wins,
					pnl: sum.TotalPnL, maxDD: sum.MaxDrawdown,
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].pnl > results[j].pnl })

	printTable(results, *top)
	printDistribution(results)
	printDefaultRank(results)
}

func printTable(results []combo, top int) {
	if top > len(results) {
		top = len(results)
	}
	fmt.Printf("\nTop %d of %d parameter combinations (Liquidity_Sweep_MTF), ranked by net P/L:\n\n", top, len(results))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "rank\tHTF\tlookback\tRR\ttrades\twin%\tnet P/L\tmaxDD")
	for i := 0; i < top; i++ {
		c := results[i]
		fmt.Fprintf(w, "%d\t%d\t%d\t%.1f\t%d\t%.1f\t%.2f\t%.2f\n",
			i+1, c.htf, c.lookback, c.rr, c.trades, c.winRate(), c.pnl, c.maxDD)
	}
	w.Flush()
}

func printDistribution(results []combo) {
	n := len(results)
	profitable := 0
	for _, c := range results {
		if c.pnl > 0 {
			profitable++
		}
	}
	// results are sorted desc by pnl; median is the middle element.
	median := results[n/2].pnl
	best := results[0].pnl
	worst := results[n-1].pnl

	fmt.Printf("\nDistribution across all %d combos:\n", n)
	fmt.Printf("  profitable:  %d / %d (%.0f%%)\n", profitable, n, float64(profitable)/float64(n)*100)
	fmt.Printf("  best:        %.2f\n", best)
	fmt.Printf("  median:      %.2f\n", median)
	fmt.Printf("  worst:       %.2f\n", worst)
	fmt.Println("\n  Read this as a robustness check: a high % profitable and a positive")
	fmt.Println("  median suggest a broad plateau (more trustworthy). A low % with the")
	fmt.Println("  best far above the median suggests an overfit spike (likely luck).")
}

func printDefaultRank(results []combo) {
	for i, c := range results {
		if c.htf == defHTF && c.lookback == defLookback && c.rr == defRR {
			fmt.Printf("\nDefault params (HTF=%d, lookback=%d, RR=%.1f) rank #%d of %d (net P/L %.2f).\n",
				defHTF, defLookback, defRR, i+1, len(results), c.pnl)
			return
		}
	}
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
