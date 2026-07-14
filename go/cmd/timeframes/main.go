// Command timeframes runs one strategy across several bar timeframes side by
// side, so you can see which timeframe (if any) an edge lives on. It aggregates
// the 5-minute base data into higher timeframes locally (day-aware, so no bar
// straddles the overnight gap) rather than re-fetching.
//
// For each timeframe it reports whole-period performance AND held-out test
// performance (the last --split fraction), because — as the out-of-sample tests
// showed — an in-sample number alone is not trustworthy.
//
// Usage:
//
//	go run ./cmd/timeframes --strategy sma --factors 1,3,6,12 --cost 0.02
//	go run ./cmd/timeframes --strategy sma --session=false --vol-threshold 0   (more trades)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"fvgbot/internal/data"
	"fvgbot/internal/engine"
	"fvgbot/internal/indicators"
	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

func main() {
	dataDir := flag.String("data", "../data", "directory containing <SYMBOL>.csv files (5-minute base bars)")
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	stratName := flag.String("strategy", "sma", "strategy: sma or liq")
	factorsCSV := flag.String("factors", "1,3,6,12", "aggregation factors on the 5-min base (1=5min, 3=15min, 6=30min, 12=60min)")
	cost := flag.Float64("cost", 0.02, "flat cost per trade side")
	split := flag.Float64("split", 0.7, "fraction used as the in-sample head; the tail is the held-out test")

	// SMA knobs (used when --strategy sma).
	smaPeriod := flag.Int("sma-period", 100, "SMA period in bars")
	volThreshold := flag.Float64("vol-threshold", 1.2, "volume filter multiple; 0 disables it (more trades)")
	// LIQ knobs (used when --strategy liq).
	rr := flag.Float64("rr", 2.0, "risk-reward")
	htf := flag.Int("htf", 3, "HTF factor")
	lookback := flag.Int("lookback", 20, "swing lookback")
	// shared.
	session := flag.Bool("session", true, "restrict entries to 09:30-12:00 ET (false = all hours, more trades)")
	flag.Parse()

	if *split <= 0 || *split >= 1 {
		log.Fatal("--split must be between 0 and 1")
	}

	symbols := splitSymbols(*symbolsCSV)
	base := map[string][]types.Bar{}
	for _, s := range symbols {
		b, err := data.LoadCSV(filepath.Join(*dataDir, s+".csv"))
		if err != nil {
			log.Printf("skipping %s: %v", s, err)
			continue
		}
		base[s] = b
	}
	if len(base) == 0 {
		log.Fatal("no data loaded")
	}

	// build constructs a freshly-configured strategy for each run.
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

	factors := parseFactors(*factorsCSV)

	fmt.Printf("\nStrategy %q across timeframes  (cost %.2f/side, test = last %.0f%%)\n\n",
		build().Name(), *cost, (1-*split)*100)
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "timeframe\ttrades\twin%\tfull P/L\ttest P/L\tP/L/trade\tmaxDD")

	for _, f := range factors {
		// Aggregate every symbol to this timeframe, run the whole period, and
		// separately run just the held-out test tail.
		var full, test []engine.TradeRecord
		for _, sym := range symbols {
			agg := indicators.AggregateByDay(base[sym], f)
			if len(agg) == 0 {
				continue
			}
			full = append(full, engine.Run(build(), sym, agg, engine.Config{CostPerTrade: *cost})...)

			cut := int(float64(len(agg)) * *split)
			test = append(test, engine.Run(build(), sym, agg[cut:], engine.Config{CostPerTrade: *cost})...)
		}

		fs := summarize(full)
		ts := summarize(test)
		perTrade := 0.0
		if fs.Trades > 0 {
			perTrade = fs.TotalPnL / float64(fs.Trades)
		}
		fmt.Fprintf(w, "%dmin\t%d\t%.1f\t%.2f\t%.2f\t%.3f\t%.2f\n",
			f*5, fs.Trades, fs.WinRate(), fs.TotalPnL, ts.TotalPnL, perTrade, fs.MaxDrawdown)
	}
	w.Flush()

	fmt.Println("\nfull P/L = whole 2-year sample (in-sample).  test P/L = held-out tail only.")
	fmt.Println("A timeframe is only interesting if its TEST P/L is positive — full P/L alone")
	fmt.Println("is the number that fooled us before.")
}

func summarize(recs []engine.TradeRecord) engine.Summary {
	if s := engine.Summarize(recs); len(s) > 0 {
		return s[0]
	}
	return engine.Summary{}
}

func parseFactors(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			log.Fatalf("bad factor %q (must be a positive integer)", p)
		}
		out = append(out, n)
	}
	return out
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
