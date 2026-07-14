// Package types holds the core data structures shared across the bot.
package types

// Bar is a single OHLCV candle. Timestamp is unix seconds.
type Bar struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
}

// Action is what a strategy or a backtest wants to do on a bar.
type Action string

const (
	Hold Action = "HOLD"
	Buy  Action = "BUY"
	Sell Action = "SELL"
)

// Signal is what a strategy returns for each bar it sees. When Action is Buy or
// Sell, StopLoss and Target define where the trade should be closed.
type Signal struct {
	Action   Action
	Size     float64 // position size; 1.0 = one unit
	StopLoss float64
	Target   float64
	Reason   string // human-readable explanation, useful for debugging
}
