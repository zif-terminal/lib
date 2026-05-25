package lighter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/httpcache"
	"github.com/zif-terminal/lib/models"
)

// TestCacheSmoke_RefreshPopulatesCacheDir wires the lighter Client through a
// refresh-mode httpcache transport and confirms a single FetchTrades call
// writes at least one entry to the configured cache dir.
func TestCacheSmoke_RefreshPopulatesCacheDir(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
		},
		nil,
	))
	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tradesResponse{
			Code:    200,
			Trades:  []lighterTrade{},
		})
	})
	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"liquidations":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	tr, err := httpcache.New(httpcache.Config{
		Mode:                     httpcache.ModeRefresh,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: httpcache.DefaultCacheableForeverPatterns,
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("httpcache.New: %v", err)
	}

	c := newTestClient(srv.URL)
	c.transport = tr

	account := &models.ExchangeAccount{
		ID:                  uuid.NewString(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	if _, _, err := c.FetchTrades(context.Background(), account, time.Time{}); err != nil {
		t.Fatalf("FetchTrades: %v", err)
	}

	count := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			count++
		}
		return nil
	})
	if count == 0 {
		t.Fatalf("expected cache dir to be populated after refresh-mode sync, got 0 entries (dir=%s)", dir)
	}
}
