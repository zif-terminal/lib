package hyperliquid

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
	if c.Name() != "hyperliquid" {
		t.Errorf("expected 'hyperliquid', got %q", c.Name())
	}
}

func TestFetchSettlementsReturnsNil(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}
	settlements, err := c.FetchSettlements(ctx, account, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settlements != nil {
		t.Errorf("expected nil settlements, got %v", settlements)
	}
}

func TestFetchHistoricalBalanceSnapshotsReturnsNil(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}
	snapshots, err := c.FetchHistoricalBalanceSnapshots(ctx, account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshots != nil {
		t.Errorf("expected nil snapshots, got %v", snapshots)
	}
}

func TestTransformFill(t *testing.T) {
	accountUUID := uuid.New()

	tests := []struct {
		name       string
		fill       hlFill
		wantBase   string
		wantMarket string
		wantSide   string
	}{
		{
			name: "perp buy",
			fill: hlFill{
				Time: 1700000000000,
				Coin: "ETH",
				Side: "B",
				Px:   "2000.50",
				Sz:   "1.5",
				Fee:  "0.30",
				Tid:  12345,
				Oid:  100,
			},
			wantBase:   "ETH",
			wantMarket: "perp",
			wantSide:   "buy",
		},
		{
			name: "perp sell",
			fill: hlFill{
				Time: 1700000000000,
				Coin: "BTC",
				Side: "A",
				Px:   "35000.00",
				Sz:   "0.1",
				Fee:  "0.50",
				Tid:  12346,
				Oid:  101,
			},
			wantBase:   "BTC",
			wantMarket: "perp",
			wantSide:   "sell",
		},
		{
			name: "spot buy",
			fill: hlFill{
				Time: 1700000000000,
				Coin: "SOL-SPOT",
				Side: "B",
				Px:   "60.00",
				Sz:   "10",
				Fee:  "0.05",
				Tid:  12347,
				Oid:  102,
			},
			wantBase:   "SOL",
			wantMarket: "spot",
			wantSide:   "buy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trade := transformFill(tt.fill, accountUUID)

			if trade.BaseAsset != tt.wantBase {
				t.Errorf("BaseAsset = %q, want %q", trade.BaseAsset, tt.wantBase)
			}
			if trade.MarketType != tt.wantMarket {
				t.Errorf("MarketType = %q, want %q", trade.MarketType, tt.wantMarket)
			}
			if trade.Side != tt.wantSide {
				t.Errorf("Side = %q, want %q", trade.Side, tt.wantSide)
			}
			if trade.QuoteAsset != "USDC" {
				t.Errorf("QuoteAsset = %q, want USDC", trade.QuoteAsset)
			}
			if trade.ExchangeAccountID != accountUUID {
				t.Errorf("ExchangeAccountID = %v, want %v", trade.ExchangeAccountID, accountUUID)
			}
		})
	}
}

func TestTransformFunding(t *testing.T) {
	accountUUID := uuid.New()

	entry := hlFundingEntry{
		Time: 1700000000000,
		Hash: "0xabc",
		Delta: hlFundingDelta{
			Coin:        "ETH",
			FundingRate: "0.0001",
			NSamples:    1,
			Usdc:        "-0.50",
		},
	}

	payment := transformFunding(entry, accountUUID)

	if payment.Type != models.TypeFunding {
		t.Errorf("Type = %q, want %q", payment.Type, models.TypeFunding)
	}
	if payment.Asset != "USDC" {
		t.Errorf("Asset = %q, want USDC", payment.Asset)
	}
	if payment.Metadata["market"] != "ETH-PERP" {
		t.Errorf("market metadata = %q, want ETH-PERP", payment.Metadata["market"])
	}
	if payment.Metadata["n_samples"] != "1" {
		t.Errorf("n_samples metadata = %q, want 1", payment.Metadata["n_samples"])
	}
}

func TestTransformFillSlashCoin(t *testing.T) {
	accountUUID := uuid.New()
	fill := hlFill{
		Time: 1700000000000,
		Coin: "PURR/USDC",
		Side: "B",
		Px:   "0.001",
		Sz:   "1000",
		Fee:  "0.01",
		Tid:  99,
		Oid:  50,
	}
	trade := transformFill(fill, accountUUID)
	if trade.BaseAsset != "PURR" {
		t.Errorf("BaseAsset = %q, want PURR", trade.BaseAsset)
	}
	if trade.MarketType != "spot" {
		t.Errorf("MarketType = %q, want spot", trade.MarketType)
	}
	if trade.QuoteAsset != "USDC" {
		t.Errorf("QuoteAsset = %q, want USDC", trade.QuoteAsset)
	}
}

func TestTransformLedgerEntry(t *testing.T) {
	accountUUID := uuid.New()

	t.Run("deposit", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xabc",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "1000.00",
			},
		}
		transfer := transformLedgerEntry(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Amount != "1000" {
			t.Errorf("Amount = %q, want 1000", transfer.Amount)
		}
	})

	t.Run("withdraw", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xdef",
			Delta: hlLedgerDelta{
				Type: "withdraw",
				Usdc: "-500.00",
			},
		}
		transfer := transformLedgerEntry(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Amount != "500" {
			t.Errorf("Amount = %q, want 500", transfer.Amount)
		}
	})

	t.Run("unsupported type returns nil", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xghi",
			Delta: hlLedgerDelta{
				Type: "internalTransfer",
				Usdc: "100.00",
			},
		}
		transfer := transformLedgerEntry(entry, accountUUID)
		if transfer != nil {
			t.Error("expected nil transfer for unsupported type")
		}
	})
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

func TestFetchTradesWithMockServer(t *testing.T) {
	fills := []hlFill{
		{
			Time: 1700000000000,
			Coin: "ETH",
			Side: "B",
			Px:   "2000.00",
			Sz:   "1.0",
			Fee:  "0.20",
			Tid:  1,
			Oid:  10,
		},
		{
			Time: 1700000001000,
			Coin: "BTC",
			Side: "A",
			Px:   "35000.00",
			Sz:   "0.5",
			Fee:  "0.50",
			Tid:  2,
			Oid:  11,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fills)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	trades, prices, err := c.FetchTrades(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prices != nil {
		t.Error("expected nil prices for Hyperliquid")
	}

	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}

	// Verify sorted ascending
	if !trades[0].Timestamp.Before(trades[1].Timestamp) {
		t.Error("trades should be sorted ascending by timestamp")
	}

	if trades[0].BaseAsset != "ETH" {
		t.Errorf("first trade base asset = %q, want ETH", trades[0].BaseAsset)
	}
	if trades[0].Side != "buy" {
		t.Errorf("first trade side = %q, want buy", trades[0].Side)
	}
}

func TestFetchFundingPaymentsWithMockServer(t *testing.T) {
	entries := []hlFundingEntry{
		{
			Time: 1700000000000,
			Hash: "0xabc",
			Delta: hlFundingDelta{
				Coin:        "ETH",
				FundingRate: "0.0001",
				NSamples:    4,
				Usdc:        "-0.50",
				Type:        "funding",
			},
		},
		{
			Time: 1700000001000,
			Hash: "0xdef",
			Delta: hlFundingDelta{
				Coin:        "BTC",
				FundingRate: "0.00005",
				NSamples:    1,
				Usdc:        "1.25",
				Type:        "funding",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	payments, err := c.FetchFundingPayments(context.Background(), account, time.UnixMilli(1700000000000))
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

	// Verify first payment
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
	if p.Metadata["n_samples"] != "4" {
		t.Errorf("n_samples metadata = %q, want 4", p.Metadata["n_samples"])
	}
	if p.ExchangeAccountID != accountID {
		t.Errorf("ExchangeAccountID = %v, want %v", p.ExchangeAccountID, accountID)
	}
}

func TestFetchDepositsWithMockServer(t *testing.T) {
	entries := []hlLedgerEntry{
		{
			Time: 1700000000000,
			Hash: "0xaaa",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "1000.00",
			},
		},
		{
			Time: 1700000001000,
			Hash: "0xbbb",
			Delta: hlLedgerDelta{
				Type: "withdraw",
				Usdc: "-500.00",
			},
		},
		{
			Time: 1700000002000,
			Hash: "0xccc",
			Delta: hlLedgerDelta{
				Type: "internalTransfer",
				Usdc: "200.00",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prices != nil {
		t.Error("expected nil prices for Hyperliquid")
	}

	// internalTransfer should be filtered out
	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfers (deposit + withdraw, skipping internalTransfer), got %d", len(transfers))
	}

	// Verify sorted ascending
	if !transfers[0].Timestamp.Before(transfers[1].Timestamp) {
		t.Error("transfers should be sorted ascending by timestamp")
	}

	// Verify deposit
	dep := transfers[0]
	if dep.Type != models.TypeDeposit {
		t.Errorf("deposit Type = %q, want %q", dep.Type, models.TypeDeposit)
	}
	if dep.Asset != "USDC" {
		t.Errorf("deposit Asset = %q, want USDC", dep.Asset)
	}
	if dep.Amount != "1000" {
		t.Errorf("deposit Amount = %q, want 1000", dep.Amount)
	}
	if dep.ExchangeAccountID != accountID {
		t.Errorf("deposit ExchangeAccountID = %v, want %v", dep.ExchangeAccountID, accountID)
	}

	// Verify withdrawal
	wd := transfers[1]
	if wd.Type != models.TypeWithdraw {
		t.Errorf("withdraw Type = %q, want %q", wd.Type, models.TypeWithdraw)
	}
	if wd.Amount != "500" {
		t.Errorf("withdraw Amount = %q, want 500 (should be positive)", wd.Amount)
	}
}

func TestDiscoverAccountsWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
				}{
					AccountValue: "5000.50",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	wallet := "0x1234567890abcdef1234567890abcdef12345678"
	accounts, err := c.DiscoverAccounts(context.Background(), wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	acc := accounts[0]
	if acc.AccountIdentifier != wallet {
		t.Errorf("AccountIdentifier = %q, want %q", acc.AccountIdentifier, wallet)
	}
	if acc.AccountType != "main" {
		t.Errorf("AccountType = %q, want main", acc.AccountType)
	}
	if acc.Name != "Main Account" {
		t.Errorf("Name = %q, want Main Account", acc.Name)
	}
}

func TestDiscoverAccountsNoActivityWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
				}{
					AccountValue: "0.00",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accounts, err := c.DiscoverAccounts(context.Background(), "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if accounts != nil {
		t.Errorf("expected nil accounts for inactive wallet, got %d", len(accounts))
	}
}

func TestFetchBalancesWithMockServer(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
				}{
					AccountValue: "5000.50",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{
				Balances: []struct {
					Coin  string `json:"coin"`
					Total string `json:"total"`
					Hold  string `json:"hold"`
				}{
					{Coin: "ETH", Total: "1.5", Hold: "0"},
				},
			})
		}
		requestCount++
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(balances) != 2 {
		t.Fatalf("expected 2 balances (USDC + ETH), got %d", len(balances))
	}

	// Find USDC and ETH balances
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
	if usdcBalance.Balance != 5000.50 {
		t.Errorf("USDC balance = %f, want 5000.50", usdcBalance.Balance)
	}

	if ethBalance == nil {
		t.Fatal("expected ETH balance")
	}
	if ethBalance.Balance != 1.5 {
		t.Errorf("ETH balance = %f, want 1.5", ethBalance.Balance)
	}
}
