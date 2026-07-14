# fvg-trading-bot

A small, polyglot trading-bot project. The first strategy is **FVG (Fair Value
Gap)**, backtested on historical intraday data before any real capital is used.

- **Go** — the backtest engine and strategies (fast, zero external dependencies,
  reads/writes plain CSV).
- **Python** — fetching historical data and analyzing results.

CSV is the interchange format between the two, so there are no binary/parquet
dependencies and the Go build needs no special flags.

## Layout

```
fvg-trading-bot/
├── data/                     # <SYMBOL>.csv price bars (generated, gitignored)
├── output/                   # results.csv from the backtest (generated)
├── go/
│   ├── cmd/backtest/         # main entrypoint
│   └── internal/
│       ├── types/            # Bar, Signal
│       ├── data/             # CSV loader
│       ├── indicators/       # SMA, swing highs/lows, volume
│       ├── strategy/         # Strategy interface + FVG
│       └── engine/           # backtest loop, results, summary
└── python/
    ├── fetch_data.py         # Yahoo Finance -> data/<SYMBOL>.csv
    └── analyze.py            # output/results.csv -> performance report
```

## Data format

`data/<SYMBOL>.csv` with a header and unix-second timestamps:

```
timestamp,open,high,low,close,volume
1715000400,183.10,183.45,182.90,183.20,150200
```

## Usage

1. Fetch data (needs `pip install -r python/requirements.txt`):

   ```
   python python/fetch_data.py --tickers AAPL MSFT NVDA --period 60d --interval 5m
   ```

2. Run the backtest (from the `go/` directory):

   ```
   go run ./cmd/backtest --symbols AAPL,MSFT,NVDA --cost 0
   ```

   Flags: `--data` (data dir), `--out` (results path), `--symbols`,
   `--cost` (flat per-side cost in price units, models commission + slippage).

3. Analyze results:

   ```
   python python/analyze.py
   ```

## Backtest assumptions

- One open position at a time.
- Entries fill at the **close** of the signal bar.
- Stop/target are only checked on **later** bars (no intrabar look-ahead). If a
  single bar spans both, the **stop** is taken first (worst case).
- Any position open at the end of data is closed at the last bar's close.
- P/L is in price units per unit of size. `--cost` subtracts a flat cost on both
  entry and exit — set it to a realistic value before trusting the numbers.
