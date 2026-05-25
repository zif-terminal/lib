package lighter

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

func TestClientImplementsInterface(t *testing.T) {
	var _ iface.ExchangeClient = (*Client)(nil)
}

func TestName(t *testing.T) {
	c := NewClient()
	if c.Name() != "lighter" {
		t.Errorf("expected 'lighter', got %q", c.Name())
	}
}

func TestFetchSettlementsReturnsNil(t *testing.T) {
	c := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0",
	}
	settlements, err := c.FetchSettlements(context.Background(), account, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settlements != nil {
		t.Errorf("expected nil settlements, got %v", settlements)
	}
}

func TestFetchHistoricalBalanceSnapshotsReturnsNil(t *testing.T) {
	c := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0",
	}
	snapshots, err := c.FetchHistoricalBalanceSnapshots(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshots != nil {
		t.Errorf("expected nil snapshots, got %v", snapshots)
	}
}

// testAPIKeyMeta returns account_type_metadata with a dummy API key for tests.
func testAPIKeyMeta() json.RawMessage {
	meta, _ := json.Marshal(map[string]interface{}{"api_key": "test-key"})
	return meta
}

// testLimiter returns a fast rate limiter for tests (no waiting).
func testLimiter() *rateLimiter {
	return newRateLimiter(1000, 1000)
}

// newTestClient creates a Client pointing at a test server URL with a fast rate limiter.
func newTestClient(url string) *Client {
	return &Client{
		baseURL:    url,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		limiter:    testLimiter(),
	}
}

// serveOrderBookDetails returns an HTTP handler that serves mock order book details.
func serveOrderBookDetails(markets []lighterOrderBookDetail, spotMarkets []lighterOrderBookDetail) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			Code:             200,
			OrderBookDetails: markets,
			SpotOrderBooks:   spotMarkets,
		})
	}
}

func TestFetchTradesWithMock(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
			{MarketID: 2, Symbol: "BTC", MarketType: "perp"},
		},
		nil,
	))

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code:    200,
			Trades: []lighterTrade{
				{
					TradeID:      1,
					TradeIDStr:   "t1",
					MarketID:     1,
					AskAccountID: 5,
					BidAccountID: 10,
					Price:        "2000.50",
					Size:         "1.5",
					UsdAmount:    "3000.75", // 2000.50 * 1.5
					TakerFee:     200,       // rate in micro-pct (2 bps)
					MakerFee:     100,       // rate in micro-pct (1 bp)
					IsMakerAsk:   true,      // ask is maker, bid (account 10) is taker
					Timestamp:    1700000001000,
					AskIDStr:     "o-ask-1",
					BidIDStr:     "o-bid-1",
				},
				{
					TradeID:      2,
					TradeIDStr:   "t2",
					MarketID:     2,
					AskAccountID: 10,
					BidAccountID: 99,
					Price:        "35000.00",
					Size:         "0.1",
					UsdAmount:    "3500", // 35000.00 * 0.1
					TakerFee:     500,    // 5 bps
					MakerFee:     100,    // 1 bp
					IsMakerAsk:   false,  // bid is maker, ask (account 10) is taker
					Timestamp:    1700000000000,
					AskIDStr:     "o-ask-2",
					BidIDStr:     "o-bid-2",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10", // account_index = 10
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	trades, prices, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}

	if len(prices) != 2 {
		t.Errorf("expected 2 price records, got %d", len(prices))
	}

	// Verify sorted ascending by timestamp
	if !trades[0].Timestamp.Before(trades[1].Timestamp) {
		t.Error("trades should be sorted ascending by timestamp")
	}

	// First trade (t2 - earlier timestamp): account 10 is ask, is_maker_ask=false so ask is taker
	sell := trades[0]
	if sell.TradeID != "t2" {
		t.Errorf("expected trade t2 first (earlier), got %s", sell.TradeID)
	}
	if sell.Side != "sell" {
		t.Errorf("expected sell side for ask account, got %s", sell.Side)
	}
	if sell.BaseAsset != "BTC" {
		t.Errorf("expected BTC, got %s", sell.BaseAsset)
	}
	if sell.MarketType != "perp" {
		t.Errorf("expected perp, got %s", sell.MarketType)
	}
	// Account 10 is ask, is_maker_ask=false, so account is taker.
	// Fee = taker_rate(500) * usd_amount(3500) / 1e6 = 1.75
	if sell.Fee != "1.75" {
		t.Errorf("expected fee 1.75 (taker rate 500 * 3500 / 1e6), got %s", sell.Fee)
	}
	if sell.OrderID != "o-ask-2" {
		t.Errorf("expected ask order ID for sell side, got %s", sell.OrderID)
	}
	if sell.FeeAsset != "USDC" {
		t.Errorf("expected USDC fee asset, got %s", sell.FeeAsset)
	}

	// Second trade (t1 - later timestamp): account 10 is bid, is_maker_ask=true so bid is taker
	buy := trades[1]
	if buy.Side != "buy" {
		t.Errorf("expected buy side for bid account, got %s", buy.Side)
	}
	if buy.BaseAsset != "ETH" {
		t.Errorf("expected ETH, got %s", buy.BaseAsset)
	}
	// Account 10 is bid, is_maker_ask=true, so bid is taker.
	// Fee = taker_rate(200) * usd_amount(3000.75) / 1e6 = 0.60015
	if buy.Fee != "0.60015" {
		t.Errorf("expected fee 0.60015 (taker rate 200 * 3000.75 / 1e6), got %s", buy.Fee)
	}
	if buy.ExchangeAccountID != accountID {
		t.Errorf("expected account ID %v, got %v", accountID, buy.ExchangeAccountID)
	}

	// Verify price records
	if len(prices) >= 2 {
		if prices[0].Source != "execution" {
			t.Errorf("price source = %s, want execution", prices[0].Source)
		}
	}
}

func TestFetchTradesSideDetermination(t *testing.T) {
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
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID:      1,
					TradeIDStr:   "t-maker-buy",
					MarketID:     1,
					AskAccountID: 99,
					BidAccountID: 5,     // Our account is bid (buyer)
					Price:        "2000",
					Size:         "1",
					UsdAmount:    "2000",
					TakerFee:     300,   // 3 bps rate
					MakerFee:     100,   // 1 bp rate
					IsMakerAsk:   false, // bid is maker
					Timestamp:    1700000000000,
					AskIDStr:     "o1",
					BidIDStr:     "o2",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "5",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}

	// Account 5 is bid, is_maker_ask=false -> bid is maker -> maker rate.
	// Fee = maker_rate(100) * usd_amount(2000) / 1e6 = 0.2
	if trades[0].Fee != "0.2" {
		t.Errorf("expected maker fee 0.2 (maker rate 100 * 2000 / 1e6), got %s", trades[0].Fee)
	}
}

// TestFetchTradesFeeRateEmpiricalSamples pins the fee derivation against the
// 6 samples cross-checked from bc9537bf's Lighter UI CSV export. taker_fee=200
// is 2 bps (200 micro-percent), and fee USDC = rate × usd_amount / 1e6.
// The first row reproduces the canonical verification case from the issue:
// TakerFee=200, UsdAmount=273.061344, BidAccountID=243407, IsMakerAsk=true →
// account is bid → bid is taker (since ask is maker) → user is taker →
// fee = 200 × 273.061344 / 1e6 = 0.054612 USDC.
func TestFetchTradesFeeRateEmpiricalSamples(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "SOL", MarketType: "perp"}},
		nil,
	))

	// All 6 CSV-verified rows. Account 243407 is on the bid side and ask is
	// maker (IsMakerAsk=true), so 243407 is always taker → rate=TakerFee=200.
	rows := []struct {
		tradeIDStr string
		usdAmount  string
		wantFee    string
	}{
		// Expected values are after cleanDecimal (trailing zeros stripped):
		// 200 × usd_amount / 1e6, rounded to 6 decimals via FloatString.
		{"s1", "273.061344", "0.054612"}, // 1.632 × 167.317 → 0.0546122688
		{"s2", "25.0836", "0.005017"},    // 0.150 × 167.224 → 0.00501672
		{"s3", "183.047865", "0.03661"},  // 1.095 × 167.167 → 0.036609573 (→ 0.036610 trimmed)
		{"s4", "209.3625", "0.041873"},   // 1.125 × 186.100 → 0.0418725
		{"s5", "209.266875", "0.041853"}, // 1.125 × 186.015 → 0.041853375
		{"s6", "29.390054", "0.005878"},  // 0.158 × 186.013 → 0.0058780108
	}

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		trs := make([]lighterTrade, 0, len(rows))
		for i, row := range rows {
			trs = append(trs, lighterTrade{
				TradeID: int64(i + 1), TradeIDStr: row.tradeIDStr, MarketID: 1,
				AskAccountID: 999999, BidAccountID: 243407,
				Price: "0", Size: "0", UsdAmount: row.usdAmount,
				TakerFee: 200, MakerFee: 0,
				IsMakerAsk: true, // ask is maker → bid (us) is taker
				Timestamp:  1700000000000 + int64(i)*1000,
				AskIDStr:   "a", BidIDStr: "b",
			})
		}
		json.NewEncoder(w).Encode(tradesResponse{Code: 200, Trades: trs})
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "243407",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != len(rows) {
		t.Fatalf("expected %d trades, got %d", len(rows), len(trades))
	}

	byID := make(map[string]string, len(trades))
	for _, tr := range trades {
		byID[tr.TradeID] = tr.Fee
	}
	for _, row := range rows {
		got, ok := byID[row.tradeIDStr]
		if !ok {
			t.Errorf("missing trade %s", row.tradeIDStr)
			continue
		}
		if got != row.wantFee {
			t.Errorf("trade %s: fee = %q, want %q (rate 200 × %s / 1e6)",
				row.tradeIDStr, got, row.wantFee, row.usdAmount)
		}
	}
}

// TestFetchTradesFeeMakerCase covers the maker-side fee path: when the user
// is the maker, rate=MakerFee and the fee is computed identically.
func TestFetchTradesFeeMakerCase(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "ETH", MarketType: "perp"}},
		nil,
	))

	// Account 243407 is the ASK and IS_MAKER_ASK=true → ask is maker → user
	// is maker → rate = MakerFee = 100. usd_amount = 273.061344.
	// Fee = 100 × 273.061344 / 1e6 = 0.027306 (6-decimal rounding).
	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID: 1, TradeIDStr: "tm", MarketID: 1,
					AskAccountID: 243407, BidAccountID: 555,
					Price: "167.317", Size: "1.632", UsdAmount: "273.061344",
					TakerFee:   200, // would be ignored (we're maker)
					MakerFee:   100,
					IsMakerAsk: true, // ask is maker → user (ask) is maker
					Timestamp:  1700000000000,
					AskIDStr:   "a1", BidIDStr: "b1",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "243407",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].Side != "sell" {
		t.Errorf("expected sell side (account is ask), got %s", trades[0].Side)
	}
	if trades[0].Fee != "0.027306" {
		t.Errorf("expected maker fee 0.027306 (rate 100 × 273.061344 / 1e6), got %s", trades[0].Fee)
	}
}

// TestFetchTradesUserNotInTradeErrors verifies that a /trades row where the
// account is on neither side returns an error rather than silently skipping
// or miscounting. Per the no-silent-skip memory.
func TestFetchTradesUserNotInTradeErrors(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "ETH", MarketType: "perp"}},
		nil,
	))

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID: 42, TradeIDStr: "42", MarketID: 1,
					AskAccountID: 111, BidAccountID: 222, // user (243407) on neither side
					Price: "100", Size: "1", UsdAmount: "100",
					TakerFee: 200, MakerFee: 100,
					IsMakerAsk: true,
					Timestamp:  1700000000000,
					AskIDStr:   "a", BidIDStr: "b",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "243407",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	_, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected error when account not on either side, got nil")
	}
	if !strings.Contains(err.Error(), "not on either side") {
		t.Errorf("expected error mentioning 'not on either side', got: %v", err)
	}
}

// TestFetchTradesInvalidUsdAmountErrors verifies a missing/malformed
// usd_amount surfaces as an error rather than a silent zero fee. Per the
// no-silent-skip memory.
func TestFetchTradesInvalidUsdAmountErrors(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "ETH", MarketType: "perp"}},
		nil,
	))

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID: 7, TradeIDStr: "7", MarketID: 1,
					AskAccountID: 99, BidAccountID: 5,
					Price: "100", Size: "1", UsdAmount: "not-a-number",
					TakerFee: 200, MakerFee: 100,
					IsMakerAsk: true,
					Timestamp:  1700000000000,
					AskIDStr:   "a", BidIDStr: "b",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "5",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	_, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected error for invalid usd_amount, got nil")
	}
	if !strings.Contains(err.Error(), "invalid usd_amount") {
		t.Errorf("expected error mentioning 'invalid usd_amount', got: %v", err)
	}
}

func TestFetchFundingPaymentsWithMock(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
		},
		nil,
	))

	mux.HandleFunc("/positionFunding", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fundingResponse{
			Code: 200,
			PositionFundings: []lighterFunding{
				{
					FundingID: 1,
					MarketID:  1,
					Change:    "-0.50",
					Timestamp: 1700000000, // Unix seconds
				},
				{
					FundingID: 2,
					MarketID:  1,
					Change:    "1.25",
					Timestamp: 1700000001, // Unix seconds
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	payments, err := c.FetchFundingPayments(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("expected 2 payments, got %d", len(payments))
	}

	// Verify sorted ascending
	if !payments[0].Timestamp.Before(payments[1].Timestamp) {
		t.Error("payments should be sorted ascending by timestamp")
	}

	p := payments[0]
	if p.Type != models.TypeFunding {
		t.Errorf("Type = %q, want %q", p.Type, models.TypeFunding)
	}
	if p.Asset != "USDC" {
		t.Errorf("Asset = %q, want USDC", p.Asset)
	}
	if p.Amount != "-0.5" {
		t.Errorf("Amount = %q, want -0.5", p.Amount)
	}
	if p.Metadata["market"] != "ETH-PERP" {
		t.Errorf("market metadata = %q, want ETH-PERP", p.Metadata["market"])
	}
	if p.Metadata["payment_id"] != "1" {
		t.Errorf("payment_id metadata = %q, want 1", p.Metadata["payment_id"])
	}
	if p.ExchangeAccountID != accountID {
		t.Errorf("ExchangeAccountID = %v, want %v", p.ExchangeAccountID, accountID)
	}
}

func TestFetchDepositsWithMock(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	// Need orderBookDetails for asset resolution (asset_id 3 -> USDC from spot markets)
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))

	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{
			Code: 200,
			Deposits: []lighterDeposit{
				{
					DepositID: "d1",
					AssetID:   3, // USDC
					Amount:    "1000.000000",
					Status:    "completed",
					Timestamp: 1700000000000,
					TxHash:    "0xaaa",
				},
				{
					DepositID: "d2",
					AssetID:   3, // USDC
					Amount:    "-500.000000",
					Status:    "completed",
					Timestamp: 1700000001000,
					TxHash:    "0xbbb",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prices != nil {
		t.Error("expected nil prices for Lighter deposits")
	}

	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(transfers))
	}

	// Verify sorted ascending
	if !transfers[0].Timestamp.Before(transfers[1].Timestamp) {
		t.Error("transfers should be sorted ascending by timestamp")
	}

	dep := transfers[0]
	if dep.Type != models.TypeDeposit {
		t.Errorf("deposit Type = %q, want %q", dep.Type, models.TypeDeposit)
	}
	if dep.Amount != "1000" {
		t.Errorf("deposit Amount = %q, want 1000", dep.Amount)
	}
	if dep.Asset != "USDC" {
		t.Errorf("deposit Asset = %q, want USDC", dep.Asset)
	}
	if dep.Metadata["deposit_id"] != "d1" {
		t.Errorf("deposit_id metadata = %q, want d1", dep.Metadata["deposit_id"])
	}

	wd := transfers[1]
	if wd.Type != models.TypeWithdraw {
		t.Errorf("withdraw Type = %q, want %q", wd.Type, models.TypeWithdraw)
	}
	if wd.Amount != "500" {
		t.Errorf("withdraw Amount = %q, want 500 (should be positive)", wd.Amount)
	}
}

// TestFetchDepositsDedupsFastWithdrawL1L2 verifies that fast L1 withdrawals
// are deduplicated against their paired L2TransferOutflow to Lighter's bridge
// (to_account_index == 3). Lighter represents a fast withdrawal as TWO API
// records: an L2TransferOutflow to the bridge (debits the user's L2 balance,
// amount is GROSS-of-fee) plus an L1 withdrawal (records the L1 receipt, net).
// Without dedup we would double-debit the principal. The L2 leg is the
// canonical record — we drop the L1.
func TestFetchDepositsDedupsFastWithdrawL1L2(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				// Bridge outflow paired with fast-PAIRED1: GROSS = L1 + 3 fee (Lighter
				// reports the explicit Fee field too — the L2-transfer loop will emit
				// the 3 USDC bridge fee separately).
				{ID: "lt-1", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: 1700000000500, Type: "L2TransferOutflow", ToAccountIndex: 3},
				// Bridge outflow paired with fast-PAIRED2: LIT-staked tier (0 fee).
				{ID: "lt-2", AssetID: 3, Amount: "5000", Fee: "0", Timestamp: 1700000009800, Type: "L2TransferOutflow", ToAccountIndex: 3},
				// Standalone non-bridge L2 outflow (different to_account_index) —
				// must NOT be considered a fast-withdraw pair.
				{ID: "lt-3", AssetID: 3, Amount: "777", Fee: "0", Timestamp: 1700000020000, Type: "L2TransferOutflow", ToAccountIndex: 999},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{
			Code: 200,
			Withdraws: []lighterWithdraw{
				// L1 settles ~500ms AFTER lt-1 bridge outflow.
				{ID: "fast-PAIRED1", AssetID: 3, Amount: "10000", Timestamp: 1700000001000, Type: "fast", Status: "completed"},
				// L1 settles ~200ms AFTER lt-2 bridge outflow.
				{ID: "fast-PAIRED2", AssetID: 3, Amount: "5000", Timestamp: 1700000010000, Type: "fast", Status: "completed"},
			},
		})
	})
	mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"transfer_fee_usdc":3000000}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotIDs := make(map[string]bool)
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
	}
	if gotIDs["withdraw_fast-PAIRED1"] {
		t.Errorf("PAIRED1 L1 withdraw should be deduped against the bridge L2 outflow lt-1")
	}
	if gotIDs["withdraw_fast-PAIRED2"] {
		t.Errorf("PAIRED2 L1 withdraw should be deduped against the bridge L2 outflow lt-2")
	}
	if !gotIDs["transfer_lt-1"] || !gotIDs["transfer_lt-2"] || !gotIDs["transfer_lt-3"] {
		t.Errorf("All L2 outflows should be present, got %v", gotIDs)
	}
	if !gotIDs["transfer_lt-1-fee"] {
		t.Errorf("Bridge fee transfer (3 USDC on lt-1) should be emitted by the L2-transfer loop")
	}
	// No synthetic bridge_fee_<id> for bridge-paired flow — the L2 record
	// already carries the explicit Fee field (handled by the L2-transfer loop).
	for id := range gotIDs {
		if strings.HasPrefix(id, "bridge_fee_") {
			t.Errorf("unexpected synthetic bridge_fee transfer %s on bridge-paired flow", id)
		}
	}
}

// TestFetchDepositsFastWithdrawSelfTransferPair verifies the second canonical
// fast-withdraw shape: an L2SelfTransfer to the same account_index (intra-
// account spot-prep) within 60s BEFORE the L1 settlement. The L2SelfTransfer
// is skipped by the normal L2 loop, so the L1 IS the canonical record. The
// (gross - net) gap is emitted as a USDC bridge_fee transfer.
func TestFetchDepositsFastWithdrawSelfTransferPair(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				// Self-transfer prep for fast-SELF-FEE: GROSS = L1 + 3 fee.
				{ID: "lt-self-1", AssetID: 3, Amount: "10003", Fee: "0", Timestamp: 1700000000000, Type: "L2SelfTransfer", ToAccountIndex: 10},
				// Self-transfer prep for fast-SELF-NOFEE: GROSS = L1 (LIT-staked tier).
				{ID: "lt-self-2", AssetID: 3, Amount: "5000", Fee: "0", Timestamp: 1700000020000, Type: "L2SelfTransfer", ToAccountIndex: 10},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{
			Code: 200,
			Withdraws: []lighterWithdraw{
				// L1 settles ~500ms after the self-transfer prep.
				{ID: "fast-SELF-FEE", AssetID: 3, Amount: "10000", Timestamp: 1700000000500, Type: "fast", Status: "completed"},
				{ID: "fast-SELF-NOFEE", AssetID: 3, Amount: "5000", Timestamp: 1700000020500, Type: "fast", Status: "completed"},
			},
		})
	})
	mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"transfer_fee_usdc":3000000}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotByID := make(map[string]*models.TransferInput)
	for _, tr := range transfers {
		gotByID[tr.ExternalID] = tr
	}

	// L1 withdraws ARE emitted (L2SelfTransfer is skipped by the L2 loop).
	if gotByID["withdraw_fast-SELF-FEE"] == nil {
		t.Errorf("withdraw_fast-SELF-FEE must be emitted (L2SelfTransfer pair, no L2 record dedup)")
	}
	if gotByID["withdraw_fast-SELF-NOFEE"] == nil {
		t.Errorf("withdraw_fast-SELF-NOFEE must be emitted")
	}
	// L2SelfTransfer is skipped by the L2 loop.
	if gotByID["transfer_lt-self-1"] != nil || gotByID["transfer_lt-self-2"] != nil {
		t.Errorf("L2SelfTransfer records must not be emitted, got %v", gotByID)
	}
	// Only the +3 USDC pair gets a synthetic fee transfer.
	feeTr := gotByID["bridge_fee_fast-SELF-FEE"]
	if feeTr == nil {
		t.Fatalf("expected bridge_fee_fast-SELF-FEE synthetic transfer")
	}
	if feeTr.Type != models.TypeFee {
		t.Errorf("bridge_fee type = %s, want %s", feeTr.Type, models.TypeFee)
	}
	if feeTr.Asset != "USDC" {
		t.Errorf("bridge_fee asset = %s, want USDC", feeTr.Asset)
	}
	gotAmt, _ := new(big.Rat).SetString(feeTr.Amount)
	wantAmt := big.NewRat(3, 1)
	if gotAmt == nil || gotAmt.Cmp(wantAmt) != 0 {
		t.Errorf("bridge_fee amount = %s, want 3", feeTr.Amount)
	}
	if feeTr.Metadata["fee_rule"] != "self-transfer-bridge" {
		t.Errorf("bridge_fee fee_rule = %q, want self-transfer-bridge", feeTr.Metadata["fee_rule"])
	}
	// LIT-staked tier (fee=0) must NOT emit a synthetic fee transfer.
	if gotByID["bridge_fee_fast-SELF-NOFEE"] != nil {
		t.Errorf("no bridge_fee transfer should be emitted when fee is 0")
	}
}

// TestFetchDepositsFastWithdrawBridgeFeeRespectsSince is a regression test for
// a uniqueness-violation bug introduced when the matcher's pair-window was
// widened: every incremental sync re-surfaced the same already-emitted L1
// fast-withdraw, and the bridge_fee emit loop re-emitted bridge_fee_fast-<id>
// rows that violated idx_transfers_external_id_unique on the second sync cycle.
//
// Verify both paths now respect `since` symmetrically for the self-transfer
// prep rule:
//   - When `since` is AFTER the fast-withdraw timestamp: NEITHER row emitted.
//   - When `since` is BEFORE the fast-withdraw timestamp: BOTH rows emitted.
func TestFetchDepositsFastWithdrawBridgeFeeRespectsSince(t *testing.T) {
	resetMarketCache()
	const oldTs int64 = 1700000000000 // ~2023-11-14, well in the past

	mkMux := func() *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
			nil,
			[]lighterOrderBookDetail{
				{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
			},
		))
		mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
		})
		// Self-transfer prep at gross = L1 + 3 USDC → emit synthetic bridge_fee.
		mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(transfersResponse{
				Code: 200,
				Transfers: []lighterTransfer{
					{ID: "lt-self-old", AssetID: 3, Amount: "10003", Fee: "0", Timestamp: oldTs - 500, Type: "L2SelfTransfer", ToAccountIndex: 10},
				},
			})
		})
		mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(withdrawsResponse{
				Code: 200,
				Withdraws: []lighterWithdraw{
					{ID: "fast-OLD", AssetID: 3, Amount: "10000", Timestamp: oldTs, Type: "fast", Status: "completed", L1TxHash: "0xabc"},
				},
			})
		})
		mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"transfer_fee_usdc":3000000}`))
		})
		return mux
	}

	newAccount := func() *models.ExchangeAccount {
		meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
		return &models.ExchangeAccount{
			ID:                  uuid.New().String(),
			AccountIdentifier:   "10",
			AccountTypeMetadata: meta,
		}
	}

	// Case 1: since is AFTER oldTs → NEITHER withdraw_fast-OLD NOR bridge_fee_fast-OLD emitted.
	{
		server := httptest.NewServer(mkMux())
		defer server.Close()
		c := newTestClient(server.URL)

		since := time.UnixMilli(oldTs).UTC().Add(1 * time.Hour)
		transfers, _, err := c.FetchDeposits(context.Background(), newAccount(), since)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, tr := range transfers {
			if tr.ExternalID == "withdraw_fast-OLD" {
				t.Errorf("withdraw_fast-OLD should be gated out by sinceMs, but was emitted")
			}
			if tr.ExternalID == "bridge_fee_fast-OLD" {
				t.Errorf("bridge_fee_fast-OLD should be gated out by sinceMs, but was emitted (this is the dup-key regression)")
			}
		}
	}

	// Case 2: since is BEFORE oldTs → BOTH withdraw_fast-OLD AND bridge_fee_fast-OLD emitted.
	{
		server := httptest.NewServer(mkMux())
		defer server.Close()
		c := newTestClient(server.URL)

		since := time.UnixMilli(oldTs).UTC().Add(-1 * time.Hour)
		transfers, _, err := c.FetchDeposits(context.Background(), newAccount(), since)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		gotByID := make(map[string]*models.TransferInput)
		for _, tr := range transfers {
			gotByID[tr.ExternalID] = tr
		}
		if gotByID["withdraw_fast-OLD"] == nil {
			t.Errorf("withdraw_fast-OLD should be emitted when since precedes ts; got %v", gotByID)
		}
		if gotByID["bridge_fee_fast-OLD"] == nil {
			t.Errorf("bridge_fee_fast-OLD should be emitted when since precedes ts; got %v", gotByID)
		}
	}
}

// TestFetchDepositsFastWithdrawSameTimestampPair is a regression test for the
// inclusive-bound bug: a bridge L2TransferOutflow at the EXACT same timestamp
// as the L1 fast-withdraw should match (Lighter atomically batches them at
// the same millisecond). The previous strict-precede (dt > 0) missed these
// and emitted both legs → double-debit.
func TestFetchDepositsFastWithdrawSameTimestampPair(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				// Bridge L2 outflow at the EXACT same timestamp as the L1
				// fast-withdraw. Must pair under inclusive bounds.
				{ID: "lt-same", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: 1700000000000, Type: "L2TransferOutflow", ToAccountIndex: 3},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{
			Code: 200,
			Withdraws: []lighterWithdraw{
				{ID: "fast-SAME", AssetID: 3, Amount: "10000", Timestamp: 1700000000000, Type: "fast", Status: "completed"},
			},
		})
	})
	mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"transfer_fee_usdc":3000000}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotIDs := make(map[string]bool)
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
	}
	// L1 must be deduped (paired with bridge L2 at same timestamp).
	if gotIDs["withdraw_fast-SAME"] {
		t.Errorf("L1 fast-SAME must be deduped against same-timestamp bridge L2 lt-same (inclusive-bound regression)")
	}
	// L2 record IS emitted (canonical).
	if !gotIDs["transfer_lt-same"] {
		t.Errorf("L2 transfer_lt-same should be emitted as the canonical outflow")
	}
	// Bridge fee on the L2 record IS emitted (fee=3).
	if !gotIDs["transfer_lt-same-fee"] {
		t.Errorf("L2 transfer_lt-same-fee should be emitted (Fee=3 on the L2 record)")
	}
	// No synthetic soft-default bridge_fee for same-timestamp paired flow.
	if gotIDs["bridge_fee_fast-SAME"] {
		t.Errorf("unexpected synthetic bridge_fee_fast-SAME — the bridge L2 already carries the fee on its Fee field")
	}
}

// TestFetchDepositsFastWithdrawBridgePairL2AfterL1 is a regression test for
// the symmetric-window fix. Production data (account 156333) showed the
// bridge L2TransferOutflow lands 195-458ms AFTER the L1 fast-withdraw — the
// previous L2-must-precede-L1 matcher missed every paired row and emitted
// both legs, double-debiting the principal on all 14 Lighter accounts.
//
// With the symmetric window, an L2 +400ms after the L1 must pair: the L1
// withdraw_fast-XXX is dropped and only the L2 transfer_<id> is emitted
// (plus its own Fee=3 USDC fee transfer). No synthetic bridge_fee.
func TestFetchDepositsFastWithdrawBridgePairL2AfterL1(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				// Bridge L2 outflow at L1.timestamp + 400ms (matches the
				// production timing observed on account 156333: 195-458ms
				// AFTER the L1). Must pair under the symmetric window.
				{ID: "lt-after", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: 1700000000400, Type: "L2TransferOutflow", ToAccountIndex: 3},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{
			Code: 200,
			Withdraws: []lighterWithdraw{
				{ID: "fast-AFTER", AssetID: 3, Amount: "10000", Timestamp: 1700000000000, Type: "fast", Status: "completed"},
			},
		})
	})
	mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"transfer_fee_usdc":3000000}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotIDs := make(map[string]bool)
	withdrawCount := 0
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
		if tr.Type == models.TypeWithdraw {
			withdrawCount++
		}
	}
	// L1 must be deduped (paired with bridge L2 that lands AFTER it).
	if gotIDs["withdraw_fast-AFTER"] {
		t.Errorf("L1 fast-AFTER must be deduped against bridge L2 lt-after that arrives +400ms later (symmetric-window regression)")
	}
	// L2 record IS emitted (canonical).
	if !gotIDs["transfer_lt-after"] {
		t.Errorf("L2 transfer_lt-after should be emitted as the canonical outflow")
	}
	// Exactly ONE withdraw record total — the L2's.
	if withdrawCount != 1 {
		t.Errorf("expected exactly 1 withdraw record (the L2's), got %d (ids=%v)", withdrawCount, gotIDs)
	}
	// No synthetic bridge_fee — the L2 record's own Fee=3 covers it via
	// the L2-transfer loop's fee transfer.
	if gotIDs["bridge_fee_fast-AFTER"] {
		t.Errorf("unexpected synthetic bridge_fee_fast-AFTER — the bridge L2 already carries the fee on its Fee field")
	}
}

// fastWithdrawTestHarness builds a mock server with the standard endpoint
// surface needed by the fast-withdraw matcher fixtures. The caller supplies the
// L1 withdraws, L2 transfers, and (optionally) the /transferFeeInfo amount.
func fastWithdrawTestHarness(t *testing.T, l1 []lighterWithdraw, l2 []lighterTransfer, feeMicro int64) (*httptest.Server, *Client, *models.ExchangeAccount) {
	t.Helper()
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{Code: 200, Transfers: l2})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{Code: 200, Withdraws: l1})
	})
	mux.HandleFunc("/transferFeeInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"transfer_fee_usdc":` + strconv.FormatInt(feeMicro, 10) + `}`))
	})
	server := httptest.NewServer(mux)
	c := newTestClient(server.URL)
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}
	return server, c, account
}

// TestFetchDepositsFastWithdrawBridgePairWithin10MinL2Lag covers the empirical
// 80ecf79e case: the bridge L2TransferOutflow lands +226s after the L1
// fast-withdraw. The previous 60s symmetric window missed it; the new
// asymmetric window (10 min late side) must pair it cleanly.
func TestFetchDepositsFastWithdrawBridgePairWithin10MinL2Lag(t *testing.T) {
	now := time.Now().UnixMilli()
	l1Ts := now - 5*60*1000 // 5 min ago (< 15 min age threshold)
	l2Ts := l1Ts + 226*1000 // +226s, matches the production trace
	l1 := []lighterWithdraw{
		{ID: "fast-LAG226", AssetID: 3, Amount: "10000", Timestamp: l1Ts, Type: "fast", Status: "completed", L1TxHash: "0xlag226"},
	}
	l2 := []lighterTransfer{
		{ID: "100001", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: l2Ts, Type: "L2TransferOutflow", ToAccountIndex: 3},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, l2, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotIDs := make(map[string]bool)
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
	}
	if gotIDs["withdraw_fast-LAG226"] {
		t.Errorf("L1 fast-LAG226 must be deduped against the +226s bridge L2")
	}
	if !gotIDs["transfer_100001"] {
		t.Errorf("L2 transfer_100001 must be emitted as the canonical outflow")
	}
}

// TestFetchDepositsFastWithdrawBridgePairBeyond10MinL2Lag verifies the
// asymmetric window's upper bound: an L2 +11 min after the L1 falls OUTSIDE
// the 10-min ceiling, no pair is found, the L1 is older than 15 min, and so
// FetchDeposits routes it down the api-orphan path (emit L1 directly +
// synthetic bridge_fee + WARN). The unrelated L2 row is also emitted on its
// own (no longer consumed by the matcher).
func TestFetchDepositsFastWithdrawBridgePairBeyond10MinL2Lag(t *testing.T) {
	// L1 is 20 min old (> 15 min age threshold) so the unmatched branch
	// takes the api-orphan path.
	now := time.Now().UnixMilli()
	l1Ts := now - 20*60*1000
	l2Ts := l1Ts + 11*60*1000 // +11 min, just past the 10-min window
	l1 := []lighterWithdraw{
		{ID: "fast-LAG11M", AssetID: 3, Amount: "10000", Timestamp: l1Ts, Type: "fast", Status: "completed", L1TxHash: "0xlag11m"},
	}
	l2 := []lighterTransfer{
		{ID: "100002", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: l2Ts, Type: "L2TransferOutflow", ToAccountIndex: 3},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, l2, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("expected no error (api-orphan path) for unmatched old fast-withdraw, got %v", err)
	}
	var sawL1, sawSyntheticFee bool
	for _, tr := range transfers {
		if tr.ExternalID == "withdraw_fast-LAG11M" {
			sawL1 = true
		}
		if tr.ExternalID == "bridge_fee_fast-LAG11M" {
			sawSyntheticFee = true
		}
	}
	if !sawL1 {
		t.Errorf("expected api-orphan L1 row withdraw_fast-LAG11M to be emitted")
	}
	if !sawSyntheticFee {
		t.Errorf("expected synthetic bridge_fee_fast-LAG11M to be emitted")
	}
}

// TestFetchDepositsFastWithdrawSelfTransferPairUnchanged preserves Rule B
// regression coverage: an L2SelfTransfer to the same account_index, gross of
// the bridge fee, with the synthetic bridge_fee_<id> emitted.
func TestFetchDepositsFastWithdrawSelfTransferPairUnchanged(t *testing.T) {
	now := time.Now().UnixMilli()
	l1Ts := now - 2*60*1000 // 2 min ago
	l1 := []lighterWithdraw{
		{ID: "fast-SELF-UNCHANGED", AssetID: 3, Amount: "10000", Timestamp: l1Ts, Type: "fast", Status: "completed", L1TxHash: "0xself"},
	}
	l2 := []lighterTransfer{
		{ID: "200001", AssetID: 3, Amount: "10003", Fee: "0", Timestamp: l1Ts - 500, Type: "L2SelfTransfer", ToAccountIndex: 10},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, l2, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotByID := make(map[string]*models.TransferInput)
	for _, tr := range transfers {
		gotByID[tr.ExternalID] = tr
	}
	if gotByID["withdraw_fast-SELF-UNCHANGED"] == nil {
		t.Errorf("L1 fast-SELF-UNCHANGED must be emitted (L2SelfTransfer is intra-account)")
	}
	feeTr := gotByID["bridge_fee_fast-SELF-UNCHANGED"]
	if feeTr == nil {
		t.Fatalf("expected synthetic bridge_fee_fast-SELF-UNCHANGED for the self-transfer-prep gap")
	}
	if feeTr.Asset != "USDC" || feeTr.Type != models.TypeFee {
		t.Errorf("bridge_fee shape wrong: asset=%s type=%s", feeTr.Asset, feeTr.Type)
	}
	if feeTr.Metadata["fee_rule"] != "self-transfer-bridge" {
		t.Errorf("bridge_fee fee_rule = %q, want self-transfer-bridge", feeTr.Metadata["fee_rule"])
	}
}

// TestFetchDepositsFastWithdrawLitStakedFeeZero covers the LIT-staked tier
// where the bridge waives the $3 fee — L2.amount == L1.amount exactly.
// The exact-match branch must pair without ever consulting /transferFeeInfo.
func TestFetchDepositsFastWithdrawLitStakedFeeZero(t *testing.T) {
	now := time.Now().UnixMilli()
	l1Ts := now - 3*60*1000
	l1 := []lighterWithdraw{
		{ID: "fast-LIT", AssetID: 3, Amount: "5000", Timestamp: l1Ts, Type: "fast", Status: "completed", L1TxHash: "0xlit"},
	}
	l2 := []lighterTransfer{
		{ID: "300001", AssetID: 3, Amount: "5000", Fee: "0", Timestamp: l1Ts + 100, Type: "L2TransferOutflow", ToAccountIndex: 3},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, l2, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotIDs := make(map[string]bool)
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
	}
	if gotIDs["withdraw_fast-LIT"] {
		t.Errorf("L1 fast-LIT must be deduped against the equal-amount bridge L2 (LIT-staked tier)")
	}
	if !gotIDs["transfer_300001"] {
		t.Errorf("L2 transfer_300001 must be emitted as the canonical outflow")
	}
}

// TestFetchDepositsFastWithdrawBurstSameAmount verifies the greedy first-fit
// consumer: two L1s with identical amounts inside the window must pair to
// distinct L2s in ascending L2.id order, with no cross-pairing.
func TestFetchDepositsFastWithdrawBurstSameAmount(t *testing.T) {
	now := time.Now().UnixMilli()
	l1aTs := now - 6*60*1000
	l1bTs := l1aTs + 2*60*1000 // 2 min after L1a, both within window
	l1 := []lighterWithdraw{
		{ID: "fast-BURST-A", AssetID: 3, Amount: "10000", Timestamp: l1aTs, Type: "fast", Status: "completed", L1TxHash: "0xa"},
		{ID: "fast-BURST-B", AssetID: 3, Amount: "10000", Timestamp: l1bTs, Type: "fast", Status: "completed", L1TxHash: "0xb"},
	}
	// Two L2 candidates; lowest id (400001) must pair with L1a (earlier ts),
	// 400002 with L1b. The L2 timestamps are AFTER both L1s, so without the
	// greedy-by-id discipline the matcher could cross-pair.
	l2 := []lighterTransfer{
		{ID: "400002", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: l1bTs + 500, Type: "L2TransferOutflow", ToAccountIndex: 3},
		{ID: "400001", AssetID: 3, Amount: "10003", Fee: "3", Timestamp: l1aTs + 500, Type: "L2TransferOutflow", ToAccountIndex: 3},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, l2, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotIDs := make(map[string]bool)
	for _, tr := range transfers {
		gotIDs[tr.ExternalID] = true
	}
	if gotIDs["withdraw_fast-BURST-A"] || gotIDs["withdraw_fast-BURST-B"] {
		t.Errorf("both L1 burst withdraws must be deduped against their bridge L2s")
	}
	if !gotIDs["transfer_400001"] || !gotIDs["transfer_400002"] {
		t.Errorf("both L2 records must be emitted as canonical outflows")
	}
}

// TestFetchDepositsFastWithdrawPendingStatus verifies pending-status L1 rows
// are deferred: no emit, no error, no fee fetch.
func TestFetchDepositsFastWithdrawPendingStatus(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "fast-PENDING", AssetID: 3, Amount: "10000", Timestamp: now - 30*60*1000, Type: "fast", Status: "pending", L1TxHash: "0xpending"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for pending L1: %v", err)
	}
	for _, tr := range transfers {
		if tr.ExternalID == "withdraw_fast-PENDING" || tr.ExternalID == "bridge_fee_fast-PENDING" {
			t.Errorf("pending L1 must not emit any transfer; got %s", tr.ExternalID)
		}
	}
}

// TestFetchDepositsFastWithdrawRefundedStatus verifies refunded-status L1 rows
// are not emitted (no real outflow occurred) and don't crash the sync.
func TestFetchDepositsFastWithdrawRefundedStatus(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "fast-REFUNDED", AssetID: 3, Amount: "10000", Timestamp: now - 30*60*1000, Type: "fast", Status: "refunded", L1TxHash: "0xrefunded"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for refunded L1: %v", err)
	}
	for _, tr := range transfers {
		if tr.ExternalID == "withdraw_fast-REFUNDED" || tr.ExternalID == "bridge_fee_fast-REFUNDED" {
			t.Errorf("refunded L1 must not emit any transfer; got %s", tr.ExternalID)
		}
	}
}

// TestFetchDepositsFastWithdrawSecureType verifies type=secure L1 rows skip
// the matcher entirely: emit the L1 as a normal withdraw, no synthetic fee,
// no crash on missing L2 partner.
func TestFetchDepositsFastWithdrawSecureType(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "secure-1", AssetID: 3, Amount: "12345", Timestamp: now - 30*60*1000, Type: "secure", Status: "completed", L1TxHash: "0xsecure"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for secure L1: %v", err)
	}
	gotByID := make(map[string]*models.TransferInput)
	for _, tr := range transfers {
		gotByID[tr.ExternalID] = tr
	}
	wd := gotByID["withdraw_secure-1"]
	if wd == nil {
		t.Fatalf("secure L1 must be emitted as a normal withdraw; got %v", gotByID)
	}
	if wd.Type != models.TypeWithdraw {
		t.Errorf("secure withdraw type=%s, want %s", wd.Type, models.TypeWithdraw)
	}
	if gotByID["bridge_fee_secure-1"] != nil {
		t.Errorf("secure L1 must not emit a synthetic bridge_fee")
	}
}

// TestFetchDepositsFastWithdrawOldUnmatchedEmitsAsOrphan verifies the
// api-orphan branch: a completed fast-withdraw older than 15 min with no L2
// partner anywhere must NOT error. Instead it must emit the L1 row directly,
// emit a synthetic bridge_fee (from /transferFeeInfo), and log a loud WARN
// tagged "api-orphan". This handles Lighter's L2TransferInflow pre-fund path
// where the exchange settles L1-only with no L2 outflow row.
func TestFetchDepositsFastWithdrawOldUnmatchedEmitsAsOrphan(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "fast-ORPHAN-OLD", AssetID: 3, Amount: "10000", Timestamp: now - 30*60*1000, Type: "fast", Status: "completed", L1TxHash: "0xorphanold"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 3000000)
	defer server.Close()

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOut)

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("expected no error for api-orphan fast-withdraw, got %v", err)
	}

	var sawL1, sawFee bool
	for _, tr := range transfers {
		switch tr.ExternalID {
		case "withdraw_fast-ORPHAN-OLD":
			sawL1 = true
			if tr.Type != models.TypeWithdraw {
				t.Errorf("orphan L1 must be TypeWithdraw, got %q", tr.Type)
			}
		case "bridge_fee_fast-ORPHAN-OLD":
			sawFee = true
			if tr.Type != models.TypeFee {
				t.Errorf("synthetic bridge_fee must be TypeFee, got %q", tr.Type)
			}
			if tr.Asset != "USDC" {
				t.Errorf("synthetic bridge_fee must be USDC, got %q", tr.Asset)
			}
			if tr.Amount != "3" && tr.Amount != "3.00000000" {
				t.Errorf("synthetic bridge_fee amount must be 3 USDC, got %q", tr.Amount)
			}
			if rule, ok := tr.Metadata["fee_rule"]; ok && rule != "api-orphan" {
				t.Errorf("synthetic bridge_fee fee_rule must be \"api-orphan\", got %q", rule)
			}
		}
	}
	if !sawL1 {
		t.Errorf("expected L1 row withdraw_fast-ORPHAN-OLD to be emitted directly")
	}
	if !sawFee {
		t.Errorf("expected synthetic bridge_fee_fast-ORPHAN-OLD to be emitted")
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "api-orphan") {
		t.Errorf("expected WARN log tagged \"api-orphan\"; got %q", logStr)
	}
	if !strings.Contains(logStr, "fast-ORPHAN-OLD") {
		t.Errorf("WARN log must mention L1 id; got %q", logStr)
	}
	if !strings.Contains(logStr, "0xorphanold") {
		t.Errorf("WARN log must mention l1_tx_hash; got %q", logStr)
	}
}

// TestFetchDepositsFastWithdrawOldUnmatchedLitWaivedEmitsZeroFee covers the
// LIT-staked tier of the api-orphan path: a completed fast-withdraw older
// than 15 min with no L2 partner AND a zero current bridge fee. The L1 row
// is still emitted; no synthetic bridge_fee row is emitted (zero-fee → no
// synthetic row needed). The WARN is still logged.
func TestFetchDepositsFastWithdrawOldUnmatchedLitWaivedEmitsZeroFee(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "fast-ORPHAN-LIT", AssetID: 3, Amount: "19797", Timestamp: now - 30*60*1000, Type: "fast", Status: "completed", L1TxHash: "0xorphanlit"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 0)
	defer server.Close()

	var logBuf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOut)

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("expected no error for LIT-waived api-orphan fast-withdraw, got %v", err)
	}

	var sawL1 bool
	for _, tr := range transfers {
		if tr.ExternalID == "withdraw_fast-ORPHAN-LIT" {
			sawL1 = true
		}
		if tr.ExternalID == "bridge_fee_fast-ORPHAN-LIT" {
			t.Errorf("LIT-waived (zero-fee) api-orphan must NOT emit a synthetic bridge_fee row")
		}
	}
	if !sawL1 {
		t.Errorf("expected L1 row withdraw_fast-ORPHAN-LIT to be emitted directly")
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "api-orphan") {
		t.Errorf("expected WARN log tagged \"api-orphan\"; got %q", logStr)
	}
	if !strings.Contains(logStr, "fast-ORPHAN-LIT") {
		t.Errorf("WARN log must mention L1 id; got %q", logStr)
	}
}

// TestFetchDepositsFastWithdrawRecentUnmatchedDefers verifies the defer
// branch: a completed fast-withdraw younger than 15 min with no L2 partner
// yet must NOT crash, and must NOT emit the L1 row this cycle (it will be
// retried next cycle when the L2 partner has likely arrived).
func TestFetchDepositsFastWithdrawRecentUnmatchedDefers(t *testing.T) {
	now := time.Now().UnixMilli()
	l1 := []lighterWithdraw{
		{ID: "fast-FRESH", AssetID: 3, Amount: "10000", Timestamp: now - 5*60*1000, Type: "fast", Status: "completed", L1TxHash: "0xfresh"},
	}
	server, c, account := fastWithdrawTestHarness(t, l1, nil, 3000000)
	defer server.Close()

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error for recent unmatched fast-withdraw: %v", err)
	}
	for _, tr := range transfers {
		if tr.ExternalID == "withdraw_fast-FRESH" {
			t.Errorf("recent unmatched fast-withdraw must be deferred (not emitted this cycle)")
		}
		if tr.ExternalID == "bridge_fee_fast-FRESH" {
			t.Errorf("deferred fast-withdraw must not emit a synthetic bridge_fee")
		}
	}
}

// TestFetchDepositsDedupsImplicitFeeAndWideWindow verifies the L1/L2 dedup
// logic handles two real-world Lighter patterns observed in production:
//
//  1. Implicit bridge fee: the L2 outflow record reports Fee=0 but its Amount
//     is gross-of-bridge-fee — i.e. L2.amount = L1.amount + ~3 USDC. Older
//     Lighter exports use this format. Without recognising it we leave the
//     L1 record in place and double-debit the principal.
//
//  2. Wide pairing gap: occasionally the L1 and L2 records land minutes
//     apart (observed up to ~227s). The previous 5-second window missed
//     these and double-counted the withdrawal.
//
//  3. False-positive resistance: when an L1 has multiple plausible matches
//     within the window (e.g. another unrelated L2 outflow at a similar
//     amount and time), we pick the closest in time so the right L2 is
//     consumed.
// TestFetchDepositsErrorsOnUnknownTransferType asserts that any L2Transfer
// type that isn't explicitly enumerated in the switch (L2TransferInflow,
// L2TransferOutflow, L2StakeAssetOutflow, L2SelfTransfer) causes FetchDeposits
// to crash-loud. Silent skipping previously hid accounting bugs — see project
// memory "Throw on unknown enum values". When Lighter introduces a new type,
// it must be added explicitly with a deliberate Deposit/Withdraw mapping.
func TestFetchDepositsErrorsOnUnknownTransferType(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				{ID: "lt-mystery", AssetID: 3, Amount: "100", Fee: "0", Timestamp: 1700000000000, Type: "L2InsuranceFundDebit"},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{Code: 200, Withdraws: []lighterWithdraw{}})
	})
	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{Code: 200, Liquidations: []lighterLiquidation{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	meta, _ := json.Marshal(map[string]interface{}{"api_key": "0xkey", "l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	_, _, err := c.FetchDeposits(context.Background(), account, time.UnixMilli(0))
	if err == nil {
		t.Fatal("expected error for unknown L2Transfer type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown L2Transfer type") {
		t.Errorf("error = %q, want substring 'unknown L2Transfer type'", err.Error())
	}
	if !strings.Contains(err.Error(), "L2InsuranceFundDebit") {
		t.Errorf("error must include the unknown type name; got %q", err.Error())
	}
}

// TestFetchDepositsMapsL2UnstakeAssetInflowToDeposit verifies that an
// L2UnstakeAssetInflow transfer (LIT returning from the fee-tier stake back to
// the spot wallet) is mapped to models.TypeDeposit. This mirrors the
// L2StakeAssetOutflow -> Withdraw mapping; without this case account_sync would
// crash-loud on unknowns and we'd miss the inflow, creating a phantom short of
// the staked asset.
func TestFetchDepositsMapsL2UnstakeAssetInflowToDeposit(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			// Map asset_id 2 -> LIT via a LIT/USDC spot market.
			{MarketID: 3001, Symbol: "LIT/USDC", MarketType: "spot", BaseAssetID: 2, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				{
					ID:        "79217479334",
					AssetID:   2,
					Amount:    "254.78000264",
					Fee:       "0",
					Timestamp: 1777786412999,
					Type:      "L2UnstakeAssetInflow",
				},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{Code: 200, Withdraws: []lighterWithdraw{}})
	})
	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{Code: 200, Liquidations: []lighterLiquidation{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	meta, _ := json.Marshal(map[string]interface{}{"api_key": "0xkey", "l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}
	tr := transfers[0]
	if tr.Type != models.TypeDeposit {
		t.Errorf("Type = %q, want %q (L2UnstakeAssetInflow must map to Deposit)", tr.Type, models.TypeDeposit)
	}
	if tr.Asset != "LIT" {
		t.Errorf("Asset = %q, want LIT", tr.Asset)
	}
	if tr.Amount != "254.78000264" {
		t.Errorf("Amount = %q, want 254.78000264", tr.Amount)
	}
	if tr.Metadata["transfer_type"] != "L2UnstakeAssetInflow" {
		t.Errorf("metadata transfer_type = %q, want L2UnstakeAssetInflow", tr.Metadata["transfer_type"])
	}
}

// TestFetchDepositsL2TransferOutflowFeeIsAlwaysUSDC verifies Fix 1: the fee
// field on a non-USDC L2TransferOutflow record is always denominated in USDC
// (Lighter's flat bridge fee), so the emitted TypeFee transfer must be marked
// as Asset=USDC — not the underlying transferred asset. Otherwise we create a
// phantom short in the transferred asset and miss the user's actual USDC
// outflow for the bridge fee.
func TestFetchDepositsL2TransferOutflowFeeIsAlwaysUSDC(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()
	// Spot order book registers asset_id 7 = LIT (any non-USDC asset works).
	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		nil,
		[]lighterOrderBookDetail{
			{MarketID: 9001, Symbol: "LIT/USDC", MarketType: "spot", BaseAssetID: 7, QuoteAssetID: 3},
		},
	))
	mux.HandleFunc("/deposit/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(depositsResponse{Code: 200, Deposits: []lighterDeposit{}})
	})
	mux.HandleFunc("/transfer/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(transfersResponse{
			Code: 200,
			Transfers: []lighterTransfer{
				// A LIT (asset_id=7) outflow with a 3-USDC bridge fee. Lighter
				// reports Fee as a USDC dollar amount regardless of the
				// transferred asset. Asset on the emitted fee transfer must be
				// USDC, not LIT.
				{ID: "lit-out-1", AssetID: 7, Amount: "100", Fee: "3", Timestamp: 1700000000000, Type: "L2TransferOutflow"},
			},
		})
	})
	mux.HandleFunc("/withdraw/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(withdrawsResponse{Code: 200, Withdraws: []lighterWithdraw{}})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var feeTr *models.TransferInput
	var bodyTr *models.TransferInput
	for _, tr := range transfers {
		if tr.ExternalID == "transfer_lit-out-1-fee" {
			feeTr = tr
		}
		if tr.ExternalID == "transfer_lit-out-1" {
			bodyTr = tr
		}
	}
	if bodyTr == nil {
		t.Fatalf("expected transfer_lit-out-1 body in transfers, got %d transfers", len(transfers))
	}
	if bodyTr.Asset != "LIT" {
		t.Errorf("body transfer asset = %s, want LIT", bodyTr.Asset)
	}
	if feeTr == nil {
		t.Fatalf("expected transfer_lit-out-1-fee in transfers, got %d transfers", len(transfers))
	}
	if feeTr.Type != models.TypeFee {
		t.Errorf("fee transfer type = %s, want %s", feeTr.Type, models.TypeFee)
	}
	if feeTr.Asset != "USDC" {
		t.Errorf("fee transfer asset = %s, want USDC (Lighter bridge fee is always USDC, regardless of transferred asset)", feeTr.Asset)
	}
	if feeTr.Amount != "3" {
		t.Errorf("fee transfer amount = %s, want 3", feeTr.Amount)
	}
}

// TestFetchDepositsDoesNotEmitLiquidationTransfers verifies that FetchDeposits
// does NOT emit any `liq_<id>-*` TransferInputs (neither `liq_<id>-fee` nor
// `liq_<id>-settlement`). Liquidation cash impact is now captured uniformly
// via the embedded trade row: trade.fee debits Balance for the taker fee, and
// the processor's FIFO close-PnL captures the position loss on the spot:USDC
// side. Together they sum to |deltaCollateral| for each liquidation by
// algebraic identity (see CHANGELOG / d7479b5d unification).
func TestFetchDepositsDoesNotEmitLiquidationTransfers(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 71, Symbol: "XPL", MarketType: "perp"},
		},
		[]lighterOrderBookDetail{
			{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3},
		},
	))

	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code: 200,
			Liquidations: []lighterLiquidation{
				{
					// notional = 4932.4 × 0.66387 = 3,273.94; rate 1.00% → fee = $32.7394
					// (fee carried on the embedded trade row, NOT emitted as a transfer)
					ID:         13334673943,
					MarketID:   71,
					Type:       "partial",
					ExecutedAt: 1760109752419,
					Trade: lighterLiquidationTrade{
						Price:    "0.66387",
						Size:     "4932.4",
						TakerFee: "1.0000",
						MakerFee: "0.0000",
					},
				},
				{
					// notional = 4201.5 × 0.69263 = 2,910.13; rate 1.00% → fee = $29.1013
					ID:         13321150729,
					MarketID:   71,
					Type:       "partial",
					ExecutedAt: 1760106957403,
					Trade: lighterLiquidationTrade{
						Price:    "0.69263",
						Size:     "4201.5",
						TakerFee: "1.0000",
						MakerFee: "0.0000",
					},
				},
				{
					// Zero-rate event.
					ID:         99999,
					MarketID:   71,
					Type:       "partial",
					ExecutedAt: 1760000000000,
					Trade: lighterLiquidationTrade{
						Price:    "1.0",
						Size:     "1.0",
						TakerFee: "0.0000",
						MakerFee: "0.0000",
					},
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No `liq_<id>-*` transfers are ever emitted by FetchDeposits after the
	// unification. Total transfer count must therefore be zero (no deposits,
	// withdraws, or transfers configured in this mux either).
	if len(transfers) != 0 {
		t.Fatalf("expected 0 transfers (no liq_<id>-* transfers ever emitted), got %d: %+v", len(transfers), transfers)
	}

	// Explicit assertion: no `liq_<id>-*` ExternalID anywhere.
	for _, tr := range transfers {
		if strings.HasPrefix(tr.ExternalID, "liq_") {
			t.Errorf("unexpected liq_* transfer %q: %+v", tr.ExternalID, tr)
		}
	}
}

// TestFetchDepositsNoLiquidationSettlementWithInfoBlocks verifies that even
// when /liquidations rows carry rich risk_info pre/post snapshots (the data
// the OLD model used to emit a `liq_<id>-settlement` transfer), FetchDeposits
// emits NO `liq_<id>-*` transfers. Liquidation cash impact now flows entirely
// through the embedded perp trade row (trade.fee + FIFO close PnL).
func TestFetchDepositsNoLiquidationSettlementWithInfoBlocks(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 71, Symbol: "XPL", MarketType: "perp"}},
		[]lighterOrderBookDetail{{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3}},
	))

	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code: 200,
			Liquidations: []lighterLiquidation{
				{
					// Full info block, |deltaCollateral| = 50, taker fee = $1.
					// Old code emitted liq_42-settlement = $50; new code emits
					// no transfer at all (cash flows via trade row).
					ID:         42,
					MarketID:   71,
					Type:       "partial",
					ExecutedAt: 1760109752419,
					Trade:      lighterLiquidationTrade{Price: "1.0", Size: "100", TakerFee: "1.00"},
					Info: lighterLiquidationInfo{
						RiskInfoBefore: lighterRiskInfo{
							CrossRiskParameters:    lighterRiskBucket{Collateral: "1050.00"},
							IsolatedRiskParameters: []lighterRiskBucket{{MarketID: 71, Collateral: "200.00"}},
						},
						RiskInfoAfter: lighterRiskInfo{
							CrossRiskParameters:    lighterRiskBucket{Collateral: "1000.00"},
							IsolatedRiskParameters: []lighterRiskBucket{{MarketID: 71, Collateral: "200.00"}},
						},
					},
				},
				{
					// No info block.
					ID:         43,
					MarketID:   71,
					Type:       "partial",
					ExecutedAt: 1760109752500,
					Trade:      lighterLiquidationTrade{Price: "1.0", Size: "100", TakerFee: "1.00"},
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tr := range transfers {
		if strings.HasPrefix(tr.ExternalID, "liq_") {
			t.Errorf("unexpected liq_* transfer %q: %+v", tr.ExternalID, tr)
		}
	}
}

// TestFetchTradesLiquidationTradeCarriesUniformFee pins the post-unification
// invariant: a `lighter-liq:`-tagged trade row carries the SAME non-zero
// trade.fee as any other perp trade (taker_fee × usd_amount / 1e6). The tag
// remains for downstream classification but no longer suppresses the fee.
// Numbers: $1,664.21 notional, taker_fee rate = 10000 (1.00%) → trade.fee =
// 10000 × 1664.21 / 1e6 = 16.6421. FetchDeposits emits no liq transfers.
func TestFetchTradesLiquidationTradeCarriesUniformFee(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 71, Symbol: "XPL", MarketType: "perp"}},
		[]lighterOrderBookDetail{{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3}},
	))

	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code: 200,
			Liquidations: []lighterLiquidation{
				{
					ID: 555, MarketID: 71, Type: "partial",
					ExecutedAt: 1760109752419,
					Trade:      lighterLiquidationTrade{Price: "1.0", Size: "1664.21", TakerFee: "1.0000"},
					Info: lighterLiquidationInfo{
						RiskInfoBefore: lighterRiskInfo{CrossRiskParameters: lighterRiskBucket{Collateral: "1030.00"}},
						RiskInfoAfter:  lighterRiskInfo{CrossRiskParameters: lighterRiskBucket{Collateral: "1000.00"}},
					},
				},
			},
		})
	})

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID: 1, TradeIDStr: "1", MarketID: 71,
					AskAccountID: 281474976666049, BidAccountID: 0,
					Price: "1.0", Size: "1664.21", UsdAmount: "1664.21",
					TakerFee:   10000,
					IsMakerAsk: false,
					Timestamp:  1760109752419,
					AskIDStr:   "a1", BidIDStr: "b1",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	// FetchTrades: the liq-tagged trade carries the uniform trade.Fee.
	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("FetchTrades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	tr := trades[0]
	if !strings.HasPrefix(tr.TxSignature, "lighter-liq:") {
		t.Errorf("trade should be tagged lighter-liq:*, got TxSignature=%q", tr.TxSignature)
	}
	// Expected fee = 10000 × 1664.21 / 1e6 = 16.6421
	if tr.Fee != "16.6421" {
		t.Errorf("trade.Fee = %q, want 16.6421 (uniform per-trade fee rate × notional)", tr.Fee)
	}

	// FetchDeposits: no liq_* transfers at all.
	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("FetchDeposits: %v", err)
	}
	for _, t2 := range transfers {
		if strings.HasPrefix(t2.ExternalID, "liq_") {
			t.Errorf("unexpected liq_* transfer %q: %+v", t2.ExternalID, t2)
		}
	}
}

// TestFetchTradesLiquidationSmallDeltaUnderTakerFee mirrors the d7479b5d WTI
// regression case: a liquidation whose embedded close is near-cost-basis
// (small |deltaCollateral| relative to taker fee). Under the unified model
// the trade row carries the full taker fee, the FIFO close PnL captures the
// small loss, and FetchDeposits emits no liq_* transfers — the algebraic
// identity guarantees the two sides sum to |deltaCollateral|.
func TestFetchTradesLiquidationSmallDeltaUnderTakerFee(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 145, Symbol: "WTI", MarketType: "perp"}},
		[]lighterOrderBookDetail{{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3}},
	))

	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code: 200,
			Liquidations: []lighterLiquidation{
				{
					ID:         67369822378,
					MarketID:   145,
					Type:       "partial",
					ExecutedAt: 1773699953000,
					Trade:      lighterLiquidationTrade{Price: "93.395", Size: "120.445", TakerFee: "1.0000"},
					Info: lighterLiquidationInfo{
						RiskInfoBefore: lighterRiskInfo{CrossRiskParameters: lighterRiskBucket{Collateral: "64582.039746"}},
						RiskInfoAfter:  lighterRiskInfo{CrossRiskParameters: lighterRiskBucket{Collateral: "64549.937138"}},
					},
				},
			},
		})
	})

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{
					TradeID: 1, TradeIDStr: "1", MarketID: 145,
					AskAccountID: 281474976666049, BidAccountID: 0,
					Price: "93.395", Size: "120.445", UsdAmount: "11249.005775",
					TakerFee:   10000, // 1.00%
					IsMakerAsk: false,
					Timestamp:  1773699953000,
					AskIDStr:   "a1", BidIDStr: "b1",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	// FetchTrades: liq-tagged trade carries trade.Fee = 10000 × 11249.005775 / 1e6 = 112.490058 (rounded to 6).
	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("FetchTrades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if !strings.HasPrefix(trades[0].TxSignature, "lighter-liq:") {
		t.Errorf("expected liq tag, got TxSignature=%q", trades[0].TxSignature)
	}
	if trades[0].Fee != "112.490058" {
		t.Errorf("liq trade.Fee = %q, want 112.490058 (taker_fee × usd_amount / 1e6)", trades[0].Fee)
	}

	// FetchDeposits: no liq_* transfers.
	transfers, _, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("FetchDeposits: %v", err)
	}
	for _, tr := range transfers {
		if strings.HasPrefix(tr.ExternalID, "liq_") {
			t.Errorf("unexpected liq_* transfer %q: %+v", tr.ExternalID, tr)
		}
	}
}

// TestFetchTradesTagsLiquidationTrades verifies that a /trades row whose
// (market_id, timestamp) matches a /liquidations row gets a TxSignature of
// "lighter-liq:<id>" so the activity processor can identify it.
func TestFetchTradesTagsLiquidationTrades(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 71, Symbol: "XPL", MarketType: "perp"}},
		[]lighterOrderBookDetail{{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3}},
	))

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				// Matches liq id=99: same market_id + timestamp.
				{
					TradeID: 1, TradeIDStr: "1", MarketID: 71,
					AskAccountID: 281474976666049, BidAccountID: 0,
					Price: "0.5", Size: "100", UsdAmount: "50", TakerFee: 100, IsMakerAsk: false,
					Timestamp: 1700000001234, AskIDStr: "a1", BidIDStr: "b1",
				},
				// Different timestamp — should NOT be tagged.
				{
					TradeID: 2, TradeIDStr: "2", MarketID: 71,
					AskAccountID: 281474976666049, BidAccountID: 0,
					Price: "0.5", Size: "100", UsdAmount: "50", TakerFee: 100, IsMakerAsk: false,
					Timestamp: 1700000002000, AskIDStr: "a2", BidIDStr: "b2",
				},
			},
		})
	})

	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code: 200,
			Liquidations: []lighterLiquidation{
				{ID: 99, MarketID: 71, Type: "partial", ExecutedAt: 1700000001234,
					Trade: lighterLiquidationTrade{TakerFee: "1.0"}},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}

	gotByTradeID := map[string]*models.TradeInput{}
	for _, tr := range trades {
		gotByTradeID[tr.TradeID] = tr
	}

	tagged := gotByTradeID["1"]
	if tagged == nil {
		t.Fatalf("missing trade_id=1 (the liquidation trade)")
	}
	if tagged.TxSignature != "lighter-liq:99" {
		t.Errorf("tagged trade TxSignature = %q, want lighter-liq:99", tagged.TxSignature)
	}

	untagged := gotByTradeID["2"]
	if untagged == nil {
		t.Fatalf("missing trade_id=2 (the non-liquidation trade)")
	}
	if untagged.TxSignature != "" {
		t.Errorf("untagged trade TxSignature = %q, want empty", untagged.TxSignature)
	}
}

// TestFetchLiquidationsPaginationCursorStallGuard verifies the cursor
// non-progression detector errors out instead of looping forever when the
// API returns the same next_cursor on consecutive pages. The liquidations
// feed is now fetched by FetchTrades (for tagging), so the guard is exercised
// via that path.
func TestFetchLiquidationsPaginationCursorStallGuard(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 71, Symbol: "XPL", MarketType: "perp"}},
		[]lighterOrderBookDetail{{MarketID: 2051, Symbol: "UNI/USDC", MarketType: "spot", BaseAssetID: 6, QuoteAssetID: 3}},
	))

	// /trades must return at least an empty payload so FetchTrades reaches
	// the liquidations fetch.
	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{Code: 200, Trades: nil})
	})

	// Always return the same non-empty cursor — this MUST trigger the stall
	// guard rather than loop forever.
	mux.HandleFunc("/liquidations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(liquidationsResponse{
			Code:       200,
			NextCursor: "STUCK",
			Liquidations: []lighterLiquidation{
				{ID: 1, MarketID: 71, Type: "partial", ExecutedAt: 1700000000000,
					Trade: lighterLiquidationTrade{TakerFee: "1.0"}},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234", "api_key": "test-key"})
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "281474976666049",
		AccountTypeMetadata: meta,
	}

	_, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected stall-guard error, got nil")
	}
	// Error wraps the pagination guard message.
	if want := "cursor did not advance"; !contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

// contains is a tiny strings.Contains shim avoiding an import (test file
// already pulls plenty; keep tests self-contained where convenient).
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFetchBalancesWithMock(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 10,
					AccountType:  0, // MAIN: USDC sourced from Collateral, non-USDC from Assets[].Balance
					L1Address:    "0x1234",
					Collateral:   "5000.50", // post-fix: this is the canonical USDC balance for MAIN accounts (Assets[].Balance can be 0 when USDC has margin_mode=enabled)
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 3, Balance: "5000.50", LockedBalance: "0"}, // ignored by post-fix code for account_type=0; Collateral takes precedence
						{Symbol: "ETH", AssetID: 1, Balance: "1.5", LockedBalance: "0.5"},
					},
				},
				{
					AccountIndex: 20,
					AccountType:  0,
					L1Address:    "0x1234",
					Collateral:   "100",
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 3, Balance: "100", LockedBalance: "0"},
					},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: meta,
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only return balances for account_index=10
	if len(balances) != 2 {
		t.Fatalf("expected 2 balances (USDC + ETH), got %d", len(balances))
	}

	var usdcBalance, ethBalance *models.BalanceSnapshot
	for _, b := range balances {
		switch b.Asset {
		case "USDC":
			usdcBalance = b
		case "ETH":
			ethBalance = b
		}
	}

	if usdcBalance == nil {
		t.Fatal("expected USDC balance")
	}
	if usdcBalance.Balance != "5000.50" {
		t.Errorf("USDC balance = %s, want 5000.50", usdcBalance.Balance)
	}

	if ethBalance == nil {
		t.Fatal("expected ETH balance")
	}
	if ethBalance.Balance != "1.5" {
		t.Errorf("ETH balance = %s, want 1.5", ethBalance.Balance)
	}
}

// TestFetchBalancesSubAccountUsesCollateral verifies that for cross-margin
// sub-accounts (account_type=1), FetchBalances reads the USDC balance from
// the top-level `collateral` field rather than `assets[].balance` (which the
// API always reports as 0 for sub-accounts).
func TestFetchBalancesSubAccountUsesCollateral(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex:     281474976624365,
					AccountType:      1,
					L1Address:        "0x1234",
					Collateral:       "1973.495134",
					AvailableBalance: "1973.495134",
					Assets: []lighterAsset{
						// Sub-account assets always show 0 balance.
						{Symbol: "USDC", AssetID: 3, Balance: "0.000000", LockedBalance: "0"},
					},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "281474976624365",
		AccountTypeMetadata: meta,
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(balances) != 1 {
		t.Fatalf("expected 1 balance (USDC from collateral), got %d", len(balances))
	}
	if balances[0].Asset != "USDC" {
		t.Errorf("expected asset USDC, got %s", balances[0].Asset)
	}
	if balances[0].Balance != "1973.495134" {
		t.Errorf("expected balance 1973.495134, got %s", balances[0].Balance)
	}
}

// TestFetchBalancesSubAccountZeroCollateral verifies that a sub-account with
// zero collateral still returns one explicit USDC=0 row, so the snapshot
// syncer writes a row even for sub-accounts that have never held an asset.
// Returning nil here was the root cause of brand-new Lighter sub-accounts
// having zero entries in spot_balance_snapshots — the syncer's
// empty-balances fallback only writes rows for previously-seen assets.
func TestFetchBalancesSubAccountZeroCollateral(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 281474976624366,
					AccountType:  1,
					L1Address:    "0x1234",
					Collateral:   "0.000000",
					Assets:       []lighterAsset{},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "281474976624366",
		AccountTypeMetadata: meta,
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(balances) != 1 {
		t.Fatalf("expected 1 USDC=0 balance for zero-collateral sub-account, got %d", len(balances))
	}
	b := balances[0]
	if b.Asset != "USDC" {
		t.Errorf("expected asset USDC, got %s", b.Asset)
	}
	if b.Balance != "0" {
		t.Errorf("expected balance 0, got %s", b.Balance)
	}
	if b.OraclePrice == nil || *b.OraclePrice != "1" {
		t.Errorf("expected oracle_price=1, got %v", b.OraclePrice)
	}
	if b.UsdValue == nil || *b.UsdValue != "0" {
		t.Errorf("expected usd_value=0, got %v", b.UsdValue)
	}
}

// TestFetchBalancesSubAccountMissingCollateralField covers the path where the
// API returns a sub-account record with no `collateral` field at all (e.g. a
// brand-new sub-account that has never been funded). The client must still
// emit a USDC=0 row so the snapshot syncer writes a starting snapshot.
func TestFetchBalancesSubAccountMissingCollateralField(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 281474976639704,
					AccountType:  1,
					L1Address:    "0x1234",
					// Collateral intentionally omitted.
					Assets: []lighterAsset{},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "281474976639704",
		AccountTypeMetadata: meta,
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(balances) != 1 {
		t.Fatalf("expected 1 USDC=0 balance for missing-collateral sub-account, got %d", len(balances))
	}
	b := balances[0]
	if b.Asset != "USDC" || b.Balance != "0" {
		t.Errorf("expected USDC=0, got %s=%s", b.Asset, b.Balance)
	}
	if b.UsdValue == nil || *b.UsdValue != "0" {
		t.Errorf("expected usd_value=0, got %v", b.UsdValue)
	}
}

// TestFetchBalancesMainAccountAllZero verifies the main-account branch
// (account_type=0) also emits a USDC=0 row when every per-asset balance is
// zero and collateral is missing/zero. Same rationale as the sub-account
// branches: every Lighter account must end up with at least one snapshot row.
func TestFetchBalancesMainAccountAllZero(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 562295,
					AccountType:  0,
					L1Address:    "0x1234",
					Collateral:   "0",
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 3, Balance: "0", LockedBalance: "0"},
						{Symbol: "ETH", AssetID: 1, Balance: "0", LockedBalance: "0"},
					},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "562295",
		AccountTypeMetadata: meta,
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(balances) != 1 {
		t.Fatalf("expected 1 USDC=0 balance for empty main account, got %d", len(balances))
	}
	if balances[0].Asset != "USDC" || balances[0].Balance != "0" {
		t.Errorf("expected USDC=0, got %s=%s", balances[0].Asset, balances[0].Balance)
	}
}

func TestDiscoverAccountsWithMock(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 0,
					L1Address:    "0xabcdef",
					Status:       1,
					Assets: []lighterAsset{
						{Symbol: "USDC", Balance: "1000"},
					},
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	accounts, err := c.DiscoverAccounts(context.Background(), "0xabcdef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	acc := accounts[0]
	if acc.AccountIdentifier != "0" {
		t.Errorf("AccountIdentifier = %q, want '0'", acc.AccountIdentifier)
	}
	if acc.AccountType != "main" {
		t.Errorf("AccountType = %q, want 'main'", acc.AccountType)
	}
	if acc.Name != "Main Account" {
		t.Errorf("Name = %q, want 'Main Account'", acc.Name)
	}
	if acc.Metadata["l1_address"] != "0xabcdef" {
		t.Errorf("metadata l1_address = %v, want '0xabcdef'", acc.Metadata["l1_address"])
	}
}

func TestDiscoverAccountsMultipleSubaccounts(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{AccountIndex: 0, L1Address: "0xabcdef"},
				{AccountIndex: 1, L1Address: "0xabcdef"},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	accounts, err := c.DiscoverAccounts(context.Background(), "0xabcdef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}

	if accounts[0].AccountType != "main" {
		t.Errorf("first account type = %q, want 'main'", accounts[0].AccountType)
	}
	if accounts[1].AccountType != "sub_account" {
		t.Errorf("second account type = %q, want 'sub_account'", accounts[1].AccountType)
	}
}

func TestDiscoverAccountsNoAccounts(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code:     200,
			Accounts: []lighterAccount{},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	accounts, err := c.DiscoverAccounts(context.Background(), "0x0000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if accounts != nil {
		t.Errorf("expected nil accounts, got %d", len(accounts))
	}
}

func TestDiscoverAccountsNotFound(t *testing.T) {
	resetMarketCache()

	// Simulate the actual Lighter API response for unknown addresses:
	// HTTP 200 with {"code":21100,"message":"account not found"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":21100,"message":"account not found"}`))
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	accounts, err := c.DiscoverAccounts(context.Background(), "0xdeadbeef")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if accounts != nil {
		t.Errorf("expected nil accounts for unknown address, got %d", len(accounts))
	}
}

func TestPagination(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
		},
		nil,
	))

	callCount := 0
	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "":
			// First page
			json.NewEncoder(w).Encode(tradesResponse{
				Code:       200,
				NextCursor: "page2",
				Trades: []lighterTrade{
					{TradeIDStr: "t1", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Size: "1", UsdAmount: "2000", TakerFee: 100, MakerFee: 50, IsMakerAsk: true, Timestamp: 1700000000000, BidIDStr: "o1", AskIDStr: "o2"},
				},
			})
		case "page2":
			// Second page (last)
			json.NewEncoder(w).Encode(tradesResponse{
				Code: 200,
				Trades: []lighterTrade{
					{TradeIDStr: "t2", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Size: "2", UsdAmount: "4002", TakerFee: 200, MakerFee: 100, IsMakerAsk: true, Timestamp: 1700000001000, BidIDStr: "o3", AskIDStr: "o4"},
				},
			})
		default:
			t.Errorf("unexpected cursor: %s", cursor)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "5",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 2 {
		t.Fatalf("expected 2 trades across 2 pages, got %d", len(trades))
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (2 pages), got %d", callCount)
	}

	if trades[0].TradeID != "t1" || trades[1].TradeID != "t2" {
		t.Errorf("trades not in expected order: %s, %s", trades[0].TradeID, trades[1].TradeID)
	}
}

func TestFetchTradesSinceFilter(t *testing.T) {
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
		json.NewEncoder(w).Encode(tradesResponse{
			Code: 200,
			Trades: []lighterTrade{
				{TradeIDStr: "old", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Size: "1", UsdAmount: "2000", TakerFee: 100, MakerFee: 50, IsMakerAsk: true, Timestamp: 1700000000000, BidIDStr: "o1", AskIDStr: "o2"},
				{TradeIDStr: "new", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Size: "1", UsdAmount: "2001", TakerFee: 100, MakerFee: 50, IsMakerAsk: true, Timestamp: 1700000002000, BidIDStr: "o3", AskIDStr: "o4"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "5",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	since := time.UnixMilli(1700000001000)
	trades, _, err := c.FetchTrades(context.Background(), account, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade after filtering, got %d", len(trades))
	}

	if trades[0].TradeID != "new" {
		t.Errorf("expected trade 'new', got %s", trades[0].TradeID)
	}
}

func TestFetchFundingPaymentsSinceBoundary(t *testing.T) {
	// Regression test: a funding payment at the exact second boundary must be
	// filtered out when since has sub-second precision (e.g., timestamp + 1ms).
	// The Lighter API returns timestamps in Unix seconds, but the syncer adds
	// +1ms to the last-seen timestamp. Without proper handling, the second-
	// precision timestamp passes the filter and causes a duplicate insert.
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{
			{MarketID: 1, Symbol: "ETH", MarketType: "perp"},
		},
		nil,
	))

	mux.HandleFunc("/positionFunding", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fundingResponse{
			Code: 200,
			PositionFundings: []lighterFunding{
				{
					FundingID: 1,
					MarketID:  1,
					Change:    "-0.50",
					Timestamp: 1700000000, // Unix seconds: exact boundary
				},
				{
					FundingID: 2,
					MarketID:  1,
					Change:    "1.25",
					Timestamp: 1700000001, // 1 second later: should pass
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                  accountID.String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	// since = exact timestamp + 1ms (simulates getSinceFundingPaymentTimestamp behavior)
	since := time.Unix(1700000000, 0).Add(1 * time.Millisecond)

	payments, err := c.FetchFundingPayments(context.Background(), account, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The funding at 1700000000 should be filtered out (it's <= since)
	// Only the funding at 1700000001 should remain
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment after boundary filtering, got %d", len(payments))
	}

	if payments[0].ExternalID != "2" {
		t.Errorf("expected funding ID 2, got %s", payments[0].ExternalID)
	}
}

func TestContextCancellation(t *testing.T) {
	c := NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0",
	}

	_, _, err := c.FetchTrades(ctx, account, time.Time{})
	if err == nil {
		t.Error("FetchTrades should respect context cancellation")
	}

	_, err = c.FetchFundingPayments(ctx, account, time.Time{})
	if err == nil {
		t.Error("FetchFundingPayments should respect context cancellation")
	}

	_, _, err = c.FetchDeposits(ctx, account, time.Time{})
	if err == nil {
		t.Error("FetchDeposits should respect context cancellation")
	}

	_, err = c.FetchBalances(ctx, account)
	if err == nil {
		t.Error("FetchBalances should respect context cancellation")
	}

	_, err = c.DiscoverAccounts(ctx, "0x1234")
	if err == nil {
		t.Error("DiscoverAccounts should respect context cancellation")
	}
}

func TestCleanDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "0"},
		{"0", "0"},
		{"1.0000", "1"},
		{"1.2300", "1.23"},
		{"-", "0"},
		{"100", "100"},
		{"-0.50", "-0.5"},
	}

	for _, tt := range tests {
		got := cleanDecimal(tt.input)
		if got != tt.want {
			t.Errorf("cleanDecimal(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMicroToDecimal(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{100000, "0.1"},
		{300000, "0.3"},
		{500000, "0.5"},
		{1000000, "1"},
		{1500000, "1.5"},
		{200, "0.0002"},
		{238, "0.000238"},
	}

	for _, tt := range tests {
		got := microToDecimal(tt.input)
		if got != tt.want {
			t.Errorf("microToDecimal(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsSuccessCode(t *testing.T) {
	if !isSuccessCode(0) {
		t.Error("code 0 should be success")
	}
	if !isSuccessCode(200) {
		t.Error("code 200 should be success")
	}
	if isSuccessCode(21100) {
		t.Error("code 21100 should not be success")
	}
	if isSuccessCode(400) {
		t.Error("code 400 should not be success")
	}
}

func TestDeriveBaseQuote(t *testing.T) {
	tests := []struct {
		symbol     string
		marketType string
		wantBase   string
		wantQuote  string
	}{
		{"ETH", "perp", "ETH", "USDC"},
		{"BTC", "perp", "BTC", "USDC"},
		{"UNI/USDC", "spot", "UNI", "USDC"},
		{"ETH/USDC", "spot", "ETH", "USDC"},
	}

	for _, tt := range tests {
		base, quote := deriveBaseQuote(tt.symbol, tt.marketType)
		if base != tt.wantBase || quote != tt.wantQuote {
			t.Errorf("deriveBaseQuote(%q, %q) = (%q, %q), want (%q, %q)",
				tt.symbol, tt.marketType, base, quote, tt.wantBase, tt.wantQuote)
		}
	}
}

func TestFetchAccountName_ReturnsNameFromAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("by"); got != "index" {
			t.Errorf("expected by=index, got %q", got)
		}
		if got := r.URL.Query().Get("value"); got != "42" {
			t.Errorf("expected value=42, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 200,
			"accounts": []map[string]interface{}{
				{"account_index": 42, "name": "My Trading Account"},
			},
		})
	}))
	defer server.Close()

	c := &Client{baseURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "42",
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "My Trading Account" {
		t.Errorf("name = %q, want %q", name, "My Trading Account")
	}
}

func TestFetchAccountName_EmptyNameReturnsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 200,
			"accounts": []map[string]interface{}{
				{"account_index": 7, "name": ""},
			},
		})
	}))
	defer server.Close()

	c := &Client{baseURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "7",
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

func TestFetchAccountName_AccountNotFoundReturnsEmpty(t *testing.T) {
	// Lighter returns HTTP 200 with code 21100 for unknown accounts.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    21100,
			"message": "account not found",
		})
	}))
	defer server.Close()

	c := &Client{baseURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "99",
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty for not-found", name)
	}
}

func TestFetchAccountName_InvalidAccountIndexErrors(t *testing.T) {
	c := NewClient()
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "not-a-number",
	}
	_, err := c.FetchAccountName(context.Background(), acct)
	if err == nil {
		t.Fatal("expected error for non-numeric account_identifier")
	}
}

// TestFetchAllFunding_CursorNonProgressionErrors verifies that the funding
// pagination loop crashes loudly if the API returns the same next_cursor it
// was given (instead of looping forever appending the same page).
func TestFetchAllFunding_CursorNonProgressionErrors(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "ETH", MarketType: "perp"}},
		nil,
	))

	// Always return the same cursor with a non-empty page — a misbehaving API.
	mux.HandleFunc("/positionFunding", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fundingResponse{
			Code:       200,
			NextCursor: "stuck-cursor",
			PositionFundings: []lighterFunding{
				{FundingID: 1, MarketID: 1, Change: "0.1", Timestamp: 1700000000},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	_, err := c.FetchFundingPayments(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected error on cursor non-progression, got nil (would have looped forever)")
	}
}

// TestFetchAllTrades_CursorNonProgressionErrors verifies the same safety check
// for the trades pagination loop.
func TestFetchAllTrades_CursorNonProgressionErrors(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", serveOrderBookDetails(
		[]lighterOrderBookDetail{{MarketID: 1, Symbol: "ETH", MarketType: "perp"}},
		nil,
	))

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradesResponse{
			Code:       200,
			NextCursor: "stuck-cursor",
			Trades: []lighterTrade{
				{TradeID: 1, TradeIDStr: "1", MarketID: 1, BidAccountID: 10, Price: "1", Size: "1", Timestamp: 1700000000000, BidIDStr: "b1"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)
	account := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   "10",
		AccountTypeMetadata: testAPIKeyMeta(),
	}

	_, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected error on cursor non-progression, got nil (would have looped forever)")
	}
}
