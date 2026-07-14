// Command backtest runs the FVG strategy over historical CSV data and writes a
// results file plus a printed summary.
//
// Usage:
//
//	go run ./cmd/backtest --data ../data --out ../output/results.csv \
//	    --symbols AAPL,MSFT,NVDA --cost 0
package main

import (
	"flag"
	"log"
	"path/filepath"
	"strings"

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/strategy"
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files")
	outPath := flag.String("out", "../output/results.csv", "path to write results CSV")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols to backtest")
	cost := flag.Float64("cost", 0, "flat cost per trade side (commission + slippage, in price units)")
	flag.Parse()

	symbols := splitSymbols(*symbolsCSV)
	if len(symbols) == 0 {
		log.Fatal("no symbols specified")
	}

	// The set of strategies to run. For now just FVG; add more here later.
	strategies := []strategy.Strategy{
		strategy.NewFVG(),
	}

	cfg := engine.Config{CostPerTrade: *cost}

	var all []engine.TradeRecord
	for _, symbol := range symbols {
		path := filepath.Join(*dataDir, symbol+".csv")
		bars, err := data.LoadCSV(path)
		if err != nil {
			log.Printf("skipping %s: %v", symbol, err)
			continue
		}
		log.Printf("loaded %d bars for %s", len(bars), symbol)

		for _, s := range strategies {
			all = append(all, engine.Run(s, symbol, bars, cfg)...)
		}
	}

	if err := engine.WriteResults(*outPath, all); err != nil {
		log.Fatalf("writing results: %v", err)
	}
	log.Printf("wrote %d rows to %s", len(all), *outPath)

	engine.PrintSummary(all)
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
