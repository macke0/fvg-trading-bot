// Command oos runs an out-of-sample test for the liquidity-sweep strategy.
//
// Each symbol's bars are split by time: the first --split fraction is the
// TRAIN segment, the rest is the TEST segment. The full parameter grid is
// searched on TRAIN, then every combo is also scored on TEST (which the search
// never saw). The gap between a combo's train and test performance is the
// overfitting you can't see from an in-sample backtest alone.
//
// The key question it answers: does picking the best parameters on train
// actually help on test? If the top-train combos do no better than average on
// test, the whole search was fitting noise.
//
// Usage:
//
//	go run ./cmd/oos --symbols AAPL,MSFT,NVDA --split 0.7 --cost 0.02
package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"fvgbot/internal/data"
	"fvgbot/internal/opt"
	"fvgbot/internal/types"
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	split := flag.Float64("split", 0.7, "fraction of each symbol's bars used for training (rest is test)")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")
	topK := flag.Int("topk", 5, "how many top-train combos to average for the selection-value check")
	flag.Parse()

	if *split <= 0 || *split >= 1 {
		log.Fatal("--split must be between 0 and 1 (exclusive)")
	}
	symbols := splitSymbols(*symbolsCSV)

	train := map[string][]types.Bar{}
	test := map[string][]types.Bar{}
	var trainBars, testBars int
	for _, sym := range symbols {
		b, err := data.LoadCSV(filepath.Join(*dataDir, sym+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", sym, err)
			continue
		}
		cut := int(float64(len(b)) * *split)
		train[sym] = b[:cut]
		test[sym] = b[cut:]
		trainBars += cut
		testBars += len(b) - cut
	}
	if len(train) == 0 {
		log.Fatal("no data loaded")
	}

	grid := opt.Grid()

	// Score every combo on both segments. TEST warmup happens inside the test
	// slice, so no train data leaks into test signals.
	type scored struct {
		p          opt.Params
		trainPnL   float64
		testPnL    float64
		testTrades int
	}
	results := make([]scored, 0, len(grid))
	var sumTestPnL float64
	for _, p := range grid {
		tr := opt.Evaluate(p, symbols, train, *cost)
		te := opt.Evaluate(p, symbols, test, *cost)
		results = append(results, scored{p: p, trainPnL: tr.TotalPnL, testPnL: te.TotalPnL, testTrades: te.Trades})
		sumTestPnL += te.TotalPnL
	}

	// Rank by TRAIN performance — this is the only info you'd have in real life.
	sort.Slice(results, func(i, j int) bool { return results[i].trainPnL > results[j].trainPnL })

	best := results[0]
	meanTestAll := sumTestPnL / float64(len(results))

	k := *topK
	if k > len(results) {
		k = len(results)
	}
	var topKTest float64
	for i := 0; i < k; i++ {
		topKTest += results[i].testPnL
	}
	meanTestTopK := topKTest / float64(k)

	// Default combo, for reference.
	var def scored
	for _, r := range results {
		if r.p == opt.Default {
			def = r
			break
		}
	}

	fmt.Printf("\nOut-of-sample test (time split at %.0f%%)\n", *split*100)
	fmt.Printf("Train: %d bars total   Test: %d bars total   (cost %.2f/side)\n\n", trainBars, testBars, *cost)

	fmt.Printf("%-26s %10s %10s   %s\n", "", "train P/L", "test P/L", "retained")
	printLine("best-on-train", best.p, best.trainPnL, best.testPnL)
	printLine("default (3,20,2.0)", def.p, def.trainPnL, def.testPnL)

	fmt.Printf("\nSelection value (does picking the best on train help on test?):\n")
	fmt.Printf("  mean test P/L of top %d train combos:  %7.2f\n", k, meanTestTopK)
	fmt.Printf("  mean test P/L of all %d combos:        %7.2f\n", len(results), meanTestAll)
	edge := meanTestTopK - meanTestAll
	fmt.Printf("  selection edge:                         %+7.2f  -> %s\n", edge, verdict(edge, best.testPnL))

	fmt.Printf("\nInterpretation:\n")
	fmt.Println("  If best-on-train LOSES on test, or the selection edge is ~0 or negative,")
	fmt.Println("  the search was fitting noise — the in-sample numbers don't carry forward.")
	fmt.Println("  A positive test P/L AND a positive selection edge is the encouraging case")
	fmt.Println("  (but 60 days is still a tiny sample — treat even that as provisional).")
}

// printLine prints one combo's train vs test P/L and the % of train P/L retained
// out of sample.
func printLine(label string, p opt.Params, trainPnL, testPnL float64) {
	retained := "n/a"
	if trainPnL > 0 {
		retained = fmt.Sprintf("%.0f%%", testPnL/trainPnL*100)
	}
	params := fmt.Sprintf("(%d,%d,%.1f)", p.HTFFactor, p.SwingLookback, p.RiskReward)
	fmt.Printf("%-16s %-9s %10.2f %10.2f   %s\n", label, params, trainPnL, testPnL, retained)
}

func verdict(edge, bestTest float64) string {
	switch {
	case bestTest <= 0:
		return "best-on-train loses out of sample (overfit)"
	case edge <= 0:
		return "selecting the best adds nothing (overfit)"
	case edge < 5:
		return "weak positive edge (inconclusive on this sample)"
	default:
		return "positive edge (params generalized, on this sample)"
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
