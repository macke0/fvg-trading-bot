// Command paper runs the strategy live against Alpaca's PAPER trading account.
//
// SAFETY:
//   - The broker endpoint is hardcoded to the PAPER host; this tool cannot reach
//     the live-trading endpoint, so no real money is ever at risk.
//   - It is observe-only by default: it logs the orders it WOULD place. Pass
//     --submit to actually place paper orders.
//   - It reads credentials from the environment (never flags):
//       APCA_API_KEY_ID, APCA_API_SECRET_KEY   (use your PAPER keys)
//
// On each new completed bar it runs the strategy; on a BUY/SELL signal it submits
// a bracket order (market entry + take-profit + stop-loss), sized to risk a fixed
// fraction of account equity. Exits are managed broker-side by the bracket legs.
//
// Usage:
//
//	go run ./cmd/paper --symbols AAPL,MSFT,NVDA --timeframe 15Min --risk 0.005
//	go run ./cmd/paper --symbols AAPL,MSFT,NVDA --submit      (actually place orders)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"fvgbot/internal/strategy"
	"fvgbot/internal/types"
)

// PAPER host only — never the live host. This is a deliberate safety constraint.
const (
	paperBase = "https://paper-api.alpaca.markets"
	dataBase  = "https://data.alpaca.markets"
)

var (
	keyID  string
	secret string
	client = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	timeframe := flag.String("timeframe", "15Min", "bar timeframe (must match the strategy you validated)")
	feed := flag.String("feed", "iex", "market-data feed: iex (free) or sip (paid)")
	interval := flag.Int("interval", 60, "seconds between polls")
	risk := flag.Float64("risk", 0.005, "fraction of equity to risk per trade")
	submit := flag.Bool("submit", false, "actually place paper orders (default: observe-only, log what it would do)")

	smaPeriod := flag.Int("sma-period", 100, "SMA period in bars")
	volThreshold := flag.Float64("vol-threshold", 0, "volume filter multiple; 0 disables it")
	rrFlag := flag.Float64("rr", 1.5, "risk-reward (unused by sma trend filter; kept for parity)")
	session := flag.Bool("session", true, "restrict entries to 09:30-12:00 ET")
	flag.Parse()
	_ = rrFlag

	keyID = os.Getenv("APCA_API_KEY_ID")
	secret = os.Getenv("APCA_API_SECRET_KEY")
	if keyID == "" || secret == "" {
		log.Fatal("missing credentials: set APCA_API_KEY_ID and APCA_API_SECRET_KEY (use your PAPER keys)")
	}

	sym := splitSymbols(*symbolsCSV)
	strat := strategy.NewSMACross()
	strat.SMAPeriod = *smaPeriod
	strat.VolumeThreshold = *volThreshold
	strat.EarlySessionOnly = *session

	acct, err := getAccount()
	if err != nil {
		log.Fatalf("cannot reach paper account: %v", err)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  PAPER TRADING (no real money)")
	fmt.Printf("  account %s   status %s   equity $%.2f   buying power $%.2f\n",
		acct.AccountNumber, acct.Status, acct.Equity, acct.BuyingPower)
	mode := "OBSERVE-ONLY (no orders placed) — pass --submit to trade"
	if *submit {
		mode = "SUBMIT (placing PAPER orders)"
	}
	fmt.Printf("  strategy %s   %s   risk %.1f%%/trade   mode: %s\n",
		strat.Name(), *timeframe, *risk*100, mode)
	fmt.Println(strings.Repeat("=", 60))

	lastBar := map[string]int64{}
	poll := time.Duration(*interval) * time.Second

	for {
		positions, orders, err := openSymbols()
		if err != nil {
			log.Printf("state check failed: %v", err)
			time.Sleep(poll)
			continue
		}

		for _, s := range sym {
			bars, err := getBars(s, *timeframe, *feed)
			if err != nil {
				log.Printf("%s: bars: %v", s, err)
				continue
			}
			bars = completedBars(bars, *timeframe)
			if len(bars) == 0 {
				continue
			}
			latest := bars[len(bars)-1]
			if latest.Timestamp == lastBar[s] {
				continue // no new completed bar
			}
			lastBar[s] = latest.Timestamp

			if positions[s] || orders[s] {
				continue // already have exposure / a working order for this symbol
			}

			sig := strat.OnBar(latest, bars)
			if sig.Action != types.Buy && sig.Action != types.Sell {
				continue
			}

			qty := sizeShares(acct.Equity, acct.BuyingPower, *risk, latest.Close, sig.StopLoss)
			when := time.Unix(latest.Timestamp, 0).UTC().Format("15:04")
			if qty < 1 {
				log.Printf("%s %s signal @%.2f but size < 1 share — skip", s, sig.Action, latest.Close)
				continue
			}

			log.Printf("%s SIGNAL %s bar %sUTC ref %.2f  qty %d  stop %.2f  target %.2f  (%s)",
				s, sig.Action, when, latest.Close, qty, sig.StopLoss, sig.Target, sig.Reason)

			if !*submit {
				continue
			}
			if err := submitBracket(s, sig.Action, qty, sig.StopLoss, sig.Target); err != nil {
				log.Printf("%s: order rejected: %v", s, err)
				continue
			}
			log.Printf("%s: bracket order submitted (qty %d)", s, qty)

			// refresh equity after a fill so the next size reflects new balance
			if a, err := getAccount(); err == nil {
				acct = a
			}
		}

		time.Sleep(poll)
	}
}

// sizeShares returns whole shares risking `risk` of equity, capped by buying power.
func sizeShares(equity, buyingPower, risk, entry, stop float64) int {
	perShare := math.Abs(entry - stop)
	if perShare <= 0 || entry <= 0 {
		return 0
	}
	shares := (equity * risk) / perShare
	if shares*entry > buyingPower {
		shares = buyingPower / entry
	}
	return int(math.Floor(shares))
}

// --- Alpaca REST helpers -------------------------------------------------

type account struct {
	AccountNumber string  `json:"account_number"`
	Status        string  `json:"status"`
	Equity        float64 `json:"equity,string"`
	BuyingPower   float64 `json:"buying_power,string"`
}

func getAccount() (account, error) {
	var a account
	err := doJSON(http.MethodGet, paperBase+"/v2/account", nil, &a)
	return a, err
}

// openSymbols returns the sets of symbols that currently have an open position
// or a working order, so we don't stack entries.
func openSymbols() (positions, orders map[string]bool, err error) {
	positions, orders = map[string]bool{}, map[string]bool{}

	var pos []struct {
		Symbol string `json:"symbol"`
	}
	if err = doJSON(http.MethodGet, paperBase+"/v2/positions", nil, &pos); err != nil {
		return nil, nil, err
	}
	for _, p := range pos {
		positions[p.Symbol] = true
	}

	var ord []struct {
		Symbol string `json:"symbol"`
	}
	if err = doJSON(http.MethodGet, paperBase+"/v2/orders?status=open&limit=500", nil, &ord); err != nil {
		return nil, nil, err
	}
	for _, o := range ord {
		orders[o.Symbol] = true
	}
	return positions, orders, nil
}

func submitBracket(symbol string, action types.Action, qty int, stop, target float64) error {
	side := "buy"
	if action == types.Sell {
		side = "sell"
	}
	body := map[string]any{
		"symbol":        symbol,
		"qty":           strconv.Itoa(qty),
		"side":          side,
		"type":          "market",
		"time_in_force": "gtc",
		"order_class":   "bracket",
		"take_profit":   map[string]string{"limit_price": price2(target)},
		"stop_loss":     map[string]string{"stop_price": price2(stop)},
	}
	return doJSON(http.MethodPost, paperBase+"/v2/orders", body, nil)
}

type alpacaBar struct {
	T string  `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V int64   `json:"v"`
}

// getBars fetches recent bars — enough history for the SMA warmup.
func getBars(symbol, timeframe, feed string) ([]types.Bar, error) {
	start := time.Now().UTC().AddDate(0, 0, -20).Format(time.RFC3339)
	q := url.Values{}
	q.Set("timeframe", timeframe)
	q.Set("start", start)
	q.Set("limit", "10000")
	q.Set("adjustment", "raw")
	q.Set("feed", feed)

	var resp struct {
		Bars []alpacaBar `json:"bars"`
	}
	u := fmt.Sprintf("%s/v2/stocks/%s/bars?%s", dataBase, url.PathEscape(symbol), q.Encode())
	if err := doJSON(http.MethodGet, u, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]types.Bar, 0, len(resp.Bars))
	for _, b := range resp.Bars {
		t, err := time.Parse(time.RFC3339, b.T)
		if err != nil {
			continue
		}
		out = append(out, types.Bar{Timestamp: t.Unix(), Open: b.O, High: b.H, Low: b.L, Close: b.C, Volume: b.V})
	}
	return out, nil
}

// completedBars drops the still-forming final bar (only fully-elapsed bars count).
func completedBars(bars []types.Bar, timeframe string) []types.Bar {
	dur := barSeconds(timeframe)
	now := time.Now().Unix()
	out := bars[:0:0]
	for _, b := range bars {
		if b.Timestamp+dur <= now {
			out = append(out, b)
		}
	}
	return out
}

func barSeconds(tf string) int64 {
	switch tf {
	case "1Min":
		return 60
	case "5Min":
		return 300
	case "15Min":
		return 900
	case "30Min":
		return 1800
	case "1Hour":
		return 3600
	default:
		return 900
	}
}

// doJSON performs an authenticated request and optionally decodes the response.
func doJSON(method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("APCA-API-KEY-ID", keyID)
	req.Header.Set("APCA-API-SECRET-KEY", secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func price2(p float64) string { return strconv.FormatFloat(p, 'f', 2, 64) }

func splitSymbols(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}
