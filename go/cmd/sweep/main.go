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
	"fvgbot/internal/opt"
	"fvgbot/internal/types"
)

// row pairs a parameter set with the performance it produced.
type row struct {
	p opt.Params
	s engine.Summary
}

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

	var results []row
	for _, p := range opt.Grid() {
		results = append(results, row{p: p, s: opt.Evaluate(p, symbols, bars, *cost)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].s.TotalPnL > results[j].s.TotalPnL })

	printTable(results, *top)
	printDistribution(results)
	printDefaultRank(results)
}

func printTable(results []row, top int) {
	if top > len(results) {
		top = len(results)
	}
	fmt.Printf("\nTop %d of %d parameter combinations (Liquidity_Sweep_MTF), ranked by net P/L:\n\n", top, len(results))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "rank\tHTF\tlookback\tRR\ttrades\twin%\tnet P/L\tmaxDD")
	for i := 0; i < top; i++ {
		r := results[i]
		fmt.Fprintf(w, "%d\t%d\t%d\t%.1f\t%d\t%.1f\t%.2f\t%.2f\n",
			i+1, r.p.HTFFactor, r.p.SwingLookback, r.p.RiskReward,
			r.s.Trades, r.s.WinRate(), r.s.TotalPnL, r.s.MaxDrawdown)
	}
	w.Flush()
}

func printDistribution(results []row) {
	n := len(results)
	profitable := 0
	for _, r := range results {
		if r.s.TotalPnL > 0 {
			profitable++
		}
	}
	median := results[n/2].s.TotalPnL
	best := results[0].s.TotalPnL
	worst := results[n-1].s.TotalPnL

	fmt.Printf("\nDistribution across all %d combos:\n", n)
	fmt.Printf("  profitable:  %d / %d (%.0f%%)\n", profitable, n, float64(profitable)/float64(n)*100)
	fmt.Printf("  best:        %.2f\n", best)
	fmt.Printf("  median:      %.2f\n", median)
	fmt.Printf("  worst:       %.2f\n", worst)
	fmt.Println("\n  Read this as a robustness check: a high % profitable and a positive")
	fmt.Println("  median suggest a broad plateau (more trustworthy). A low % with the")
	fmt.Println("  best far above the median suggests an overfit spike (likely luck).")
}

func printDefaultRank(results []row) {
	for i, r := range results {
		if r.p == opt.Default {
			fmt.Printf("\nDefault params (HTF=%d, lookback=%d, RR=%.1f) rank #%d of %d (net P/L %.2f).\n",
				opt.Default.HTFFactor, opt.Default.SwingLookback, opt.Default.RiskReward,
				i+1, len(results), r.s.TotalPnL)
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
