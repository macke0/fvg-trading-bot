"""Fetch historical OHLCV bars from Yahoo Finance and write them as CSV.

The CSV layout matches what the Go engine expects:

    timestamp,open,high,low,close,volume

where timestamp is unix seconds. One file per ticker: data/<TICKER>.csv

Usage:
    python fetch_data.py --tickers AAPL MSFT NVDA --period 60d --interval 5m
"""

import argparse
import os

import pandas as pd
import yfinance as yf


def repo_data_dir() -> str:
    base_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    return os.path.join(base_dir, "data")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tickers", nargs="+", default=["AAPL", "MSFT", "NVDA"])
    parser.add_argument("--period", default="60d")
    parser.add_argument("--interval", default="5m")
    args = parser.parse_args()

    period = args.period
    # Yahoo caps intraday 5m history at 60 days.
    if args.interval == "5m" and period not in ("1d", "5d", "1mo", "60d"):
        print("Note: 5m data is limited to 60 days; using period='60d'.")
        period = "60d"

    print(
        f"Fetching {', '.join(args.tickers)} "
        f"(period={period}, interval={args.interval})..."
    )
    data = yf.download(
        args.tickers, period=period, interval=args.interval, group_by="ticker"
    )
    if data is None or data.empty:
        print("No data returned. Check tickers / connection.")
        return

    target_dir = repo_data_dir()
    os.makedirs(target_dir, exist_ok=True)

    for ticker in args.tickers:
        frame = data if len(args.tickers) == 1 else data[ticker]
        frame = frame.dropna().reset_index()

        datetime_col = frame.columns[0]
        out = pd.DataFrame()
        out["timestamp"] = frame[datetime_col].astype("int64") // 10**9
        out["open"] = frame["Open"].astype("float64")
        out["high"] = frame["High"].astype("float64")
        out["low"] = frame["Low"].astype("float64")
        out["close"] = frame["Close"].astype("float64")
        out["volume"] = frame["Volume"].astype("int64")

        path = os.path.join(target_dir, f"{ticker}.csv")
        out.to_csv(path, index=False)
        print(f"  {ticker}: {len(out)} bars -> {path}")


if __name__ == "__main__":
    main()
