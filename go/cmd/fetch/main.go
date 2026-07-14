// Command fetch downloads historical OHLCV bars from Alpaca's market-data API
// and writes them as data/<SYMBOL>.csv in the format the engine expects.
//
// It reads credentials from the environment (never from flags, so keys don't
// end up in your shell history):
//
//	APCA_API_KEY_ID     — your Alpaca API key id
//	APCA_API_SECRET_KEY — your Alpaca API secret
//
// Get free keys at https://app.alpaca.markets (Home -> API Keys). The free tier
// uses the IEX feed: prices are accurate for liquid names, volume is a partial
// (IEX-only) figure.
//
// Usage:
//
//	go run ./cmd/fetch --symbols AAPL,MSFT,NVDA --timeframe 5Min \
//	    --start 2022-01-01 --end 2024-01-01
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const barsURLTemplate = "https://data.alpaca.markets/v2/stocks/%s/bars"

// alpacaBar is one bar as returned by the API (fields we care about).
type alpacaBar struct {
	T string  `json:"t"` // RFC3339 timestamp
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V int64   `json:"v"`
}

type barsResponse struct {
	Bars          []alpacaBar `json:"bars"`
	NextPageToken string      `json:"next_page_token"`
}

func main() {
	symbolsCSV := flag.String("symbols", "AAPL,MSFT,NVDA", "comma-separated symbols")
	timeframe := flag.String("timeframe", "5Min", "Alpaca timeframe: 1Min, 5Min, 15Min, 1Hour, 1Day, ...")
	startStr := flag.String("start", "", "start date YYYY-MM-DD (default: 2 years ago)")
	endStr := flag.String("end", "", "end date YYYY-MM-DD (default: today)")
	feed := flag.String("feed", "iex", "data feed: iex (free) or sip (paid subscription)")
	outDir := flag.String("out", "../data", "directory to write <SYMBOL>.csv files")
	flag.Parse()

	keyID := os.Getenv("APCA_API_KEY_ID")
	secret := os.Getenv("APCA_API_SECRET_KEY")
	if keyID == "" || secret == "" {
		log.Fatal("missing credentials: set APCA_API_KEY_ID and APCA_API_SECRET_KEY " +
			"(create free keys at https://app.alpaca.markets -> API Keys)")
	}

	start, err := parseDate(*startStr, time.Now().AddDate(-2, 0, 0))
	if err != nil {
		log.Fatalf("bad --start: %v", err)
	}
	end, err := parseDate(*endStr, time.Now())
	if err != nil {
		log.Fatalf("bad --end: %v", err)
	}
	startRFC := start.UTC().Format(time.RFC3339)
	endRFC := end.UTC().Format(time.RFC3339)

	client := &http.Client{Timeout: 30 * time.Second}

	for _, symbol := range splitSymbols(*symbolsCSV) {
		bars, err := fetchBars(client, symbol, *timeframe, startRFC, endRFC, *feed, keyID, secret)
		if err != nil {
			log.Printf("%s: %v", symbol, err)
			continue
		}
		if len(bars) == 0 {
			log.Printf("%s: no bars returned for the given range/feed", symbol)
			continue
		}
		path := filepath.Join(*outDir, symbol+".csv")
		if err := writeCSV(path, bars); err != nil {
			log.Printf("%s: writing csv: %v", symbol, err)
			continue
		}
		log.Printf("%s: %d bars (%s .. %s) -> %s", symbol, len(bars), bars[0].T, bars[len(bars)-1].T, path)
	}
}

// fetchBars pulls every bar in [start, end], following pagination tokens.
func fetchBars(client *http.Client, symbol, timeframe, start, end, feed, keyID, secret string) ([]alpacaBar, error) {
	var all []alpacaBar
	pageToken := ""

	for {
		q := url.Values{}
		q.Set("timeframe", timeframe)
		q.Set("start", start)
		q.Set("end", end)
		q.Set("limit", "10000")
		q.Set("adjustment", "raw")
		q.Set("feed", feed)
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}

		reqURL := fmt.Sprintf(barsURLTemplate, url.PathEscape(symbol)) + "?" + q.Encode()
		req, err := http.NewRequest(http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("APCA-API-KEY-ID", keyID)
		req.Header.Set("APCA-API-SECRET-KEY", secret)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(2 * time.Second) // rate limited: back off and retry the same page
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, fmt.Errorf("auth rejected (status %d) — check your API key/secret and that the %q feed is allowed on your plan", resp.StatusCode, feed)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var r barsResponse
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		resp.Body.Close()

		all = append(all, r.Bars...)
		if r.NextPageToken == "" {
			break
		}
		pageToken = r.NextPageToken
	}

	return all, nil
}

func writeCSV(path string, bars []alpacaBar) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		return err
	}
	for _, b := range bars {
		t, err := time.Parse(time.RFC3339, b.T)
		if err != nil {
			continue // skip a bar with an unparseable timestamp rather than fail the file
		}
		row := []string{
			strconv.FormatInt(t.Unix(), 10),
			strconv.FormatFloat(b.O, 'f', 4, 64),
			strconv.FormatFloat(b.H, 'f', 4, 64),
			strconv.FormatFloat(b.L, 'f', 4, 64),
			strconv.FormatFloat(b.C, 'f', 4, 64),
			strconv.FormatInt(b.V, 10),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// parseDate parses YYYY-MM-DD, or returns def if s is empty.
func parseDate(s string, def time.Time) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return def, nil
	}
	return time.Parse("2006-01-02", strings.TrimSpace(s))
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
