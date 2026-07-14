# fvg-trading-bot

A small, polyglot trading-bot project. Strategies are backtested on historical
intraday data before any real capital is used. The first strategy was **FVG
(Fair Value Gap)**; **SMA-cross** and **liquidity-sweep** followed.

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

## Strategies

All three take entries only in the early US session (09:30–12:00 ET) and place a
stop just beyond a structural level, with a target at a multiple of that risk.

- **`fvg` — FVG_Trend.** Finds a fair value gap (a 3-bar imbalance) in the recent
  past, then enters when price returns to test it and holds, in the direction of
  the trend (price vs. 100-SMA). Risk-reward 1.5.
- **`sma` — SMA_Cross_Volume.** Enters when price crosses the 100-SMA on
  above-average volume; stop beyond the recent swing. Risk-reward 2.0.
- **`liq` — Liquidity_Sweep_MTF.** Aggregates the 5-minute bars into a
  higher timeframe, finds its swing highs/lows, and enters when a bar wicks past
  a level (sweeping liquidity) but closes back inside and the next bar confirms.
  Risk-reward 2.0.

## Data format

`data/<SYMBOL>.csv` with a header and unix-second timestamps:

```
timestamp,open,high,low,close,volume
1715000400,183.10,183.45,182.90,183.20,150200
```

## Usage

1. Fetch data. Two options:

   **a) Alpaca (recommended — years of history, pure Go, no Python).** Get free
   API keys at <https://app.alpaca.markets> (Home → API Keys), then set them as
   environment variables and run `cmd/fetch`:

   ```bash
   # bash / WSL
   export APCA_API_KEY_ID=your_key_id
   export APCA_API_SECRET_KEY=your_secret
   ```
   ```powershell
   # Windows PowerShell
   $env:APCA_API_KEY_ID = "your_key_id"
   $env:APCA_API_SECRET_KEY = "your_secret"
   ```
   ```
   # from the go/ directory
   go run ./cmd/fetch --symbols AAPL,MSFT,NVDA --timeframe 5Min --start 2022-01-01 --end 2024-01-01
   ```

   The free tier uses the IEX feed: prices are accurate for liquid names, but
   volume is IEX-only (understated). Flags: `--symbols`, `--timeframe`
   (`1Min`/`5Min`/`15Min`/`1Hour`/`1Day`), `--start`/`--end` (`YYYY-MM-DD`),
   `--feed` (`iex` free / `sip` paid), `--out` (data dir). Keys are read from the
   environment only — never passed as flags.

   **b) Yahoo Finance (no signup, but capped at 60 days for intraday):**

   ```
   python python/fetch_data.py --tickers AAPL MSFT NVDA --period 60d --interval 5m
   ```

2. Run the backtest (from the `go/` directory):

   ```
   go run ./cmd/backtest --symbols AAPL,MSFT,NVDA --strategies all --cost 0
   ```

   Flags:
   - `--symbols` — comma-separated tickers (default `AAPL,MSFT,NVDA`)
   - `--strategies` — which to run: `fvg`, `sma`, `liq`, or `all` (default `all`)
   - `--cost` — flat per-side cost in price units (models commission + slippage)
   - `--data` — data directory (default `../data`)
   - `--out` — results path (default `../output/results.csv`)

3. Analyze results:

   ```
   python python/analyze.py
   ```

4. Plot an equity curve (pure Go, no Python needed — writes an SVG):

   ```
   go run ./cmd/plot --in ../output/results.csv --out ../output/equity.svg
   ```

5. Sweep liquidity-sweep parameters to check robustness / overfitting:

   ```
   go run ./cmd/sweep --symbols AAPL,MSFT,NVDA --cost 0.02 --top 15
   ```

   It prints the best combos plus a distribution (how many of the grid were
   profitable, the median, and where the default params rank) — a broad plateau
   is more trustworthy than a lone spike.

6. Out-of-sample test — the honesty check for the sweep:

   ```
   go run ./cmd/oos --symbols AAPL,MSFT,NVDA --split 0.7 --cost 0.02
   ```

   Splits each symbol by time, searches the grid on the TRAIN segment, then
   scores every combo on the unseen TEST segment. If the best-on-train combo
   loses on test (it does, on this data), the sweep was fitting noise — a
   built-in warning against trusting optimized in-sample results.

7. Compare timeframes side by side (aggregates the 5-min base into higher
   timeframes, day-aware so no bar spans the overnight gap):

   ```
   go run ./cmd/timeframes --strategy sma --factors 1,3,6,12 --cost 0.02
   ```

   Each row shows whole-period P/L AND held-out test P/L (last `--split`), so an
   in-sample number can't fool you. Strategy knobs let you trade off frequency
   vs. edge: `--sma-period`, `--vol-threshold` (0 disables the filter),
   `--session=false` (all hours), and for liq `--rr`/`--htf`/`--lookback`.

## Backtest assumptions

- One open position at a time.
- Entries fill at the **close** of the signal bar.
- Stop/target are only checked on **later** bars (no intrabar look-ahead). If a
  single bar spans both, the **stop** is taken first (worst case).
- Any position open at the end of data is closed at the last bar's close.
- P/L is in price units per unit of size. `--cost` subtracts a flat cost on both
  entry and exit — set it to a realistic value before trusting the numbers.
