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

func TestFetchTradesWithMock(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			OrderBooks: []lighterOrderBookDetail{
				{MarketID: 1, Symbol: "ETH", BaseAsset: "ETH", QuoteAsset: "USDC", MarketType: "perp"},
				{MarketID: 2, Symbol: "BTC", BaseAsset: "BTC", QuoteAsset: "USDC", MarketType: "perp"},
			},
		})
	})

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse[lighterTrade]{
			Code:    0,
			Message: "ok",
			Data: []lighterTrade{
				{
					TradeID:      "t1",
					MarketID:     1,
					AskAccountID: 5,
					BidAccountID: 10,
					Price:        "2000.50",
					Quantity:     "1.5",
					TakerFee:     "0.30",
					MakerFee:     "0.10",
					IsBuyerTaker: true,
					Timestamp:    1700000001000,
					AskOrderID:   "o-ask-1",
					BidOrderID:   "o-bid-1",
				},
				{
					TradeID:      "t2",
					MarketID:     2,
					AskAccountID: 10,
					BidAccountID: 99,
					Price:        "35000.00",
					Quantity:     "0.1",
					TakerFee:     "0.50",
					MakerFee:     "0.15",
					IsBuyerTaker: false,
					Timestamp:    1700000000000,
					AskOrderID:   "o-ask-2",
					BidOrderID:   "o-bid-2",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "10", // account_index = 10
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

	// First trade (t2 - earlier timestamp): account 10 is ask, so sell
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
	if sell.Fee != "0.5" {
		t.Errorf("expected fee 0.5 (taker, sell side, buyer is not taker), got %s", sell.Fee)
	}
	if sell.OrderID != "o-ask-2" {
		t.Errorf("expected ask order ID for sell side, got %s", sell.OrderID)
	}
	if sell.FeeAsset != "USDC" {
		t.Errorf("expected USDC fee asset, got %s", sell.FeeAsset)
	}

	// Second trade (t1 - later timestamp): account 10 is bid, so buy
	buy := trades[1]
	if buy.Side != "buy" {
		t.Errorf("expected buy side for bid account, got %s", buy.Side)
	}
	if buy.BaseAsset != "ETH" {
		t.Errorf("expected ETH, got %s", buy.BaseAsset)
	}
	if buy.Fee != "0.3" {
		t.Errorf("expected fee 0.3 (taker, buy side, buyer is taker), got %s", buy.Fee)
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

	mux.HandleFunc("/orderBookDetails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			OrderBooks: []lighterOrderBookDetail{
				{MarketID: 1, Symbol: "ETH", BaseAsset: "ETH", QuoteAsset: "USDC", MarketType: "perp"},
			},
		})
	})

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse[lighterTrade]{
			Code: 0,
			Data: []lighterTrade{
				{
					TradeID:      "t-maker-buy",
					MarketID:     1,
					AskAccountID: 99,
					BidAccountID: 5, // Our account is bid (buyer) and maker
					Price:        "2000",
					Quantity:     "1",
					TakerFee:     "0.30",
					MakerFee:     "0.10",
					IsBuyerTaker: false, // Buyer is maker
					Timestamp:    1700000000000,
					AskOrderID:   "o1",
					BidOrderID:   "o2",
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "5",
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}

	// Account 5 is bid (buyer), buyer is NOT taker, so fee should be maker fee
	if trades[0].Fee != "0.1" {
		t.Errorf("expected maker fee 0.1, got %s", trades[0].Fee)
	}
}

func TestFetchFundingPaymentsWithMock(t *testing.T) {
	resetMarketCache()
	mux := http.NewServeMux()

	mux.HandleFunc("/orderBookDetails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			OrderBooks: []lighterOrderBookDetail{
				{MarketID: 1, Symbol: "ETH", BaseAsset: "ETH", QuoteAsset: "USDC", MarketType: "perp"},
			},
		})
	})

	mux.HandleFunc("/positionFunding", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse[lighterFunding]{
			Code: 0,
			Data: []lighterFunding{
				{
					FundingID: "f1",
					AccountID: 10,
					MarketID:  1,
					Amount:    "-0.50",
					Timestamp: 1700000000000,
				},
				{
					FundingID: "f2",
					AccountID: 10,
					MarketID:  1,
					Amount:    "1.25",
					Timestamp: 1700000001000,
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "10",
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
	if p.Amount != "-0.50" {
		t.Errorf("Amount = %q, want -0.50", p.Amount)
	}
	if p.Metadata["market"] != "ETH-PERP" {
		t.Errorf("market metadata = %q, want ETH-PERP", p.Metadata["market"])
	}
	if p.Metadata["payment_id"] != "f1" {
		t.Errorf("payment_id metadata = %q, want f1", p.Metadata["payment_id"])
	}
	if p.ExchangeAccountID != accountID {
		t.Errorf("ExchangeAccountID = %v, want %v", p.ExchangeAccountID, accountID)
	}
}

func TestFetchDepositsWithMock(t *testing.T) {
	resetMarketCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse[lighterDeposit]{
			Code: 0,
			Data: []lighterDeposit{
				{
					DepositID: "d1",
					AccountID: 10,
					AssetID:   0,
					Amount:    "1000.00",
					Status:    "completed",
					Timestamp: 1700000000000,
					TxHash:    "0xaaa",
				},
				{
					DepositID: "d2",
					AccountID: 10,
					AssetID:   0,
					Amount:    "-500.00",
					Status:    "completed",
					Timestamp: 1700000001000,
					TxHash:    "0xbbb",
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(server.URL)

	accountID := uuid.New()
	meta, _ := json.Marshal(map[string]interface{}{"l1_address": "0x1234"})
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
			Accounts: []lighterAccount{
				{
					AccountIndex: 10,
					L1Address:    "0x1234",
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 0, Balance: "5000.50", LockedBalance: "0"},
						{Symbol: "ETH", AssetID: 1, Balance: "1.5", LockedBalance: "0.5"},
					},
				},
				{
					AccountIndex: 20,
					L1Address:    "0x1234",
					Assets: []lighterAsset{
						{Symbol: "USDC", AssetID: 0, Balance: "100", LockedBalance: "0"},
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
					Status:       "active",
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

	mux.HandleFunc("/orderBookDetails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			OrderBooks: []lighterOrderBookDetail{
				{MarketID: 1, Symbol: "ETH", BaseAsset: "ETH", QuoteAsset: "USDC", MarketType: "perp"},
			},
		})
	})

	callCount := 0
	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "":
			// First page
			json.NewEncoder(w).Encode(apiResponse[lighterTrade]{
				Code:       0,
				NextCursor: "page2",
				Data: []lighterTrade{
					{TradeID: "t1", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Quantity: "1", TakerFee: "0.1", MakerFee: "0.05", IsBuyerTaker: true, Timestamp: 1700000000000, BidOrderID: "o1", AskOrderID: "o2"},
				},
			})
		case "page2":
			// Second page (last)
			json.NewEncoder(w).Encode(apiResponse[lighterTrade]{
				Code: 0,
				Data: []lighterTrade{
					{TradeID: "t2", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Quantity: "2", TakerFee: "0.2", MakerFee: "0.1", IsBuyerTaker: true, Timestamp: 1700000001000, BidOrderID: "o3", AskOrderID: "o4"},
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
		ID:                uuid.New().String(),
		AccountIdentifier: "5",
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

	mux.HandleFunc("/orderBookDetails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lighterOrderBookDetailsResp{
			OrderBooks: []lighterOrderBookDetail{
				{MarketID: 1, Symbol: "ETH", BaseAsset: "ETH", QuoteAsset: "USDC", MarketType: "perp"},
			},
		})
	})

	mux.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apiResponse[lighterTrade]{
			Code: 0,
			Data: []lighterTrade{
				{TradeID: "old", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2000", Quantity: "1", TakerFee: "0.1", MakerFee: "0.05", IsBuyerTaker: true, Timestamp: 1700000000000, BidOrderID: "o1", AskOrderID: "o2"},
				{TradeID: "new", MarketID: 1, BidAccountID: 5, AskAccountID: 99, Price: "2001", Quantity: "1", TakerFee: "0.1", MakerFee: "0.05", IsBuyerTaker: true, Timestamp: 1700000002000, BidOrderID: "o3", AskOrderID: "o4"},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.URL)

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "5",
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
