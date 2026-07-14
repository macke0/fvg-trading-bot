"""Analyze backtest results written by the Go engine (output/results.csv).

Reports, per strategy: number of closed trades, win rate, total P/L, average
win / average loss, profit factor, and max drawdown.

Usage:
    python analyze.py
"""

import os

import pandas as pd


def results_path() -> str:
    base_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    return os.path.join(base_dir, "output", "results.csv")


def main() -> None:
    path = results_path()
    if not os.path.exists(path):
        print(f"No results file at {path}. Run the Go backtest first.")
        return

    df = pd.read_csv(path)
    if df.empty:
        print("Results file is empty.")
        return

    closed = df[df["Action"].str.startswith("CLOSE")].copy()
    if closed.empty:
        print("No closed trades found.")
        return

    print("=" * 60)
    print("FVG BACKTEST RESULTS")
    print("=" * 60)

    for strat in closed["Strategy"].unique():
        trades = closed[closed["Strategy"] == strat].sort_values("Timestamp")
        n = len(trades)
        wins = trades[trades["ProfitLoss"] > 0]
        losses = trades[trades["ProfitLoss"] <= 0]

        win_rate = len(wins) / n * 100 if n else 0.0
        total_pnl = trades["ProfitLoss"].sum()
        avg_win = wins["ProfitLoss"].mean() if not wins.empty else 0.0
        avg_loss = losses["ProfitLoss"].mean() if not losses.empty else 0.0

        gross_win = wins["ProfitLoss"].sum()
        gross_loss = -losses["ProfitLoss"].sum()
        profit_factor = gross_win / gross_loss if gross_loss > 0 else float("inf")

        cumulative = trades["ProfitLoss"].cumsum()
        peak = cumulative.expanding(min_periods=1).max()
        max_drawdown = (peak - cumulative).max()

        print(f"\n--- {strat} ---")
        print(f"Closed trades:  {n}")
        print(f"Win rate:       {win_rate:.2f}%")
        print(f"Total P/L:      {total_pnl:.2f}")
        print(f"Avg win:        {avg_win:.2f}")
        print(f"Avg loss:       {avg_loss:.2f}")
        print(f"Profit factor:  {profit_factor:.2f}")
        print(f"Max drawdown:   {max_drawdown:.2f}")


if __name__ == "__main__":
    main()
