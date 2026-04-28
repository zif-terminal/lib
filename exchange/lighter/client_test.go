package lighter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
					TakerFee:     300000, // 0.3 USDC in micro-USDC
					MakerFee:     100000, // 0.1 USDC in micro-USDC
					IsMakerAsk:   true,   // ask is maker
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
					TakerFee:     500000, // 0.5 USDC
					MakerFee:     150000, // 0.15 USDC
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
	// Account 10 is ask, is_maker_ask=false, so account is taker -> taker fee
	if sell.Fee != "0.5" {
		t.Errorf("expected fee 0.5 (taker fee for ask when is_maker_ask=false), got %s", sell.Fee)
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
	// Account 10 is bid, is_maker_ask=true, so bid is taker -> taker fee
	if buy.Fee != "0.3" {
		t.Errorf("expected fee 0.3 (taker fee for bid when is_maker_ask=true), got %s", buy.Fee)
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
					BidAccountID: 5,      // Our account is bid (buyer)
					Price:        "2000",
					Size:         "1",
					TakerFee:     300000, // 0.3 USDC
					MakerFee:     100000, // 0.1 USDC
					IsMakerAsk:   false,  // bid is maker
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

	// Account 5 is bid, is_maker_ask=false -> bid is maker -> maker fee
	if trades[0].Fee != "0.1" {
		t.Errorf("expected maker fee 0.1, got %s", trades[0].Fee)
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

func TestFetchBalancesWithMock(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterAccountResp{
			Code: 200,
			Accounts: []lighterAccount{
				{
					AccountIndex: 10,
					L1Address:    "0x1234",
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 3, Balance: "5000.50", LockedBalance: "0"},
						{Symbol: "ETH", AssetID: 1, Balance: "1.5", LockedBalance: "0.5"},
					},
				},
				{
					AccountIndex: 20,
					L1Address:    "0x1234",
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
	if usdcBalance.WalletType != "spot" {
		t.Errorf("USDC WalletType = %q, want \"spot\"", usdcBalance.WalletType)
	}

	if ethBalance == nil {
		t.Fatal("expected ETH balance")
	}
	if ethBalance.Balance != "1.5" {
		t.Errorf("ETH balance = %s, want 1.5", ethBalance.Balance)
	}
	if ethBalance.WalletType != "spot" {
		t.Errorf("ETH WalletType = %q, want \"spot\"", ethBalance.WalletType)
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
	if balances[0].WalletType != "perp" {
		t.Errorf("expected WalletType=perp for sub-account collateral, got %q", balances[0].WalletType)
	}
}

// TestFetchBalancesSubAccountEmitsPerpPositions verifies that for sub-accounts
// (account_type=1) FetchBalances emits both the USDC collateral row AND one
// row per non-zero open perp position, all tagged wallet_type=perp. Position
// sizes are signed so shorts pass through as negatives.
func TestFetchBalancesSubAccountEmitsPerpPositions(t *testing.T) {
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
					AvailableBalance: "1500.00",
					Positions: []lighterPosition{
						{Symbol: "BTC", MarketID: 1, Position: "-0.5", AvgEntryPrice: "65000"},
						{Symbol: "ETH", MarketID: 2, Position: "1.2", AvgEntryPrice: "3500"},
						{Symbol: "SOL", MarketID: 3, Position: "0", AvgEntryPrice: "0"}, // zero — skipped
					},
					Assets: []lighterAsset{
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

	if len(balances) != 3 {
		t.Fatalf("expected 3 balances (USDC perp + BTC + ETH), got %d", len(balances))
	}

	byAsset := map[string]*models.BalanceSnapshot{}
	for _, b := range balances {
		byAsset[b.Asset] = b
	}

	usdc, ok := byAsset["USDC"]
	if !ok {
		t.Fatal("expected USDC perp collateral row")
	}
	if usdc.WalletType != "perp" {
		t.Errorf("USDC WalletType = %q, want \"perp\"", usdc.WalletType)
	}
	if usdc.Balance != "1973.495134" {
		t.Errorf("USDC Balance = %q, want 1973.495134", usdc.Balance)
	}

	btc, ok := byAsset["BTC"]
	if !ok {
		t.Fatal("expected BTC position row")
	}
	if btc.WalletType != "perp" {
		t.Errorf("BTC WalletType = %q, want \"perp\"", btc.WalletType)
	}
	if btc.Balance != "-0.5" {
		t.Errorf("BTC Balance = %q, want -0.5 (signed)", btc.Balance)
	}

	eth, ok := byAsset["ETH"]
	if !ok {
		t.Fatal("expected ETH position row")
	}
	if eth.WalletType != "perp" {
		t.Errorf("ETH WalletType = %q, want \"perp\"", eth.WalletType)
	}
	if eth.Balance != "1.2" {
		t.Errorf("ETH Balance = %q, want 1.2", eth.Balance)
	}

	if _, present := byAsset["SOL"]; present {
		t.Error("SOL row should not be present (zero position)")
	}
}

// TestFetchBalancesSubAccountZeroCollateral verifies that a sub-account with
// zero collateral returns an empty slice (so the snapshot syncer's
// empty-balances path can write zero snapshots for previously seen assets).
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
	if len(balances) != 0 {
		t.Fatalf("expected 0 balances for zero-collateral sub-account, got %d", len(balances))
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
					{TradeIDStr: "t1", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Size: "1", TakerFee: 100000, MakerFee: 50000, IsMakerAsk: true, Timestamp: 1700000000000, BidIDStr: "o1", AskIDStr: "o2"},
				},
			})
		case "page2":
			// Second page (last)
			json.NewEncoder(w).Encode(tradesResponse{
				Code: 200,
				Trades: []lighterTrade{
					{TradeIDStr: "t2", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Size: "2", TakerFee: 200000, MakerFee: 100000, IsMakerAsk: true, Timestamp: 1700000001000, BidIDStr: "o3", AskIDStr: "o4"},
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
				{TradeIDStr: "old", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Size: "1", TakerFee: 100000, MakerFee: 50000, IsMakerAsk: true, Timestamp: 1700000000000, BidIDStr: "o1", AskIDStr: "o2"},
				{TradeIDStr: "new", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Size: "1", TakerFee: 100000, MakerFee: 50000, IsMakerAsk: true, Timestamp: 1700000002000, BidIDStr: "o3", AskIDStr: "o4"},
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
