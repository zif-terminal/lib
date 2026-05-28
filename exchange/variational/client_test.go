package variational

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// mockDBClient implements the subset of db.DBClient needed by variational.Client.
type mockDBClient struct {
	db.DBClient // embed to satisfy the interface; unused methods will panic

	listOmniRawEventsFunc        func(ctx context.Context, accountID uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error)
	listOmniRawEventsByTypesFunc func(ctx context.Context, accountID uuid.UUID, eventTypes []string, sinceMs int64) ([]*db.OmniRawEvent, error)
}

func (m *mockDBClient) ListOmniRawEvents(ctx context.Context, accountID uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
	if m.listOmniRawEventsFunc != nil {
		return m.listOmniRawEventsFunc(ctx, accountID, eventType, sinceMs)
	}
	return nil, nil
}

func (m *mockDBClient) ListOmniRawEventsByTypes(ctx context.Context, accountID uuid.UUID, eventTypes []string, sinceMs int64) ([]*db.OmniRawEvent, error) {
	if m.listOmniRawEventsByTypesFunc != nil {
		return m.listOmniRawEventsByTypesFunc(ctx, accountID, eventTypes, sinceMs)
	}
	return nil, nil
}

func strPtr(s string) *string { return &s }

func makeTradeEvent(omniID, underlying, side, price, qty string, tsMs int64) *db.OmniRawEvent {
	return &db.OmniRawEvent{
		ID:                uuid.New(),
		ExchangeAccountID: uuid.Nil,
		OmniID:            omniID,
		TimestampMs:       tsMs,
		EventType:         "trade",
		Side:              strPtr(side),
		InstrumentType:    strPtr("perpetual_future"),
		Underlying:        strPtr(underlying),
		Price:             strPtr(price),
		Qty:               strPtr(qty),
		TradeType:         strPtr("trade"),
		RawData:           json.RawMessage(`{}`),
	}
}

func makeTransferEvent(omniID, eventType, qty, asset string, tsMs int64) *db.OmniRawEvent {
	ev := &db.OmniRawEvent{
		ID:                uuid.New(),
		ExchangeAccountID: uuid.Nil,
		OmniID:            omniID,
		TimestampMs:       tsMs,
		EventType:         eventType,
		Asset:             strPtr(asset),
		Qty:               strPtr(qty),
		Status:            strPtr("confirmed"),
		RawData:           json.RawMessage(`{}`),
	}
	if eventType == "fee" {
		ev.FeeType = strPtr("deposit")
	}
	return ev
}

func makeFundingEvent(omniID, underlying, qty, fundingRate string, tsMs int64) *db.OmniRawEvent {
	return &db.OmniRawEvent{
		ID:                uuid.New(),
		ExchangeAccountID: uuid.Nil,
		OmniID:            omniID,
		TimestampMs:       tsMs,
		EventType:         "funding",
		Asset:             strPtr("USDC"),
		Underlying:        strPtr(underlying),
		Qty:               strPtr(qty),
		FundingRate:       strPtr(fundingRate),
		RawData:           json.RawMessage(`{}`),
	}
}

func testAccount() *models.ExchangeAccount {
	return &models.ExchangeAccount{
		ID: uuid.New().String(),
		Exchange: &models.Exchange{
			Name:           "variational",
			DisplayName:    "Variational",
			SettlesOnClose: true,
		},
	}
}

func TestName(t *testing.T) {
	c := NewClient(&mockDBClient{})
	if c.Name() != "variational" {
		t.Errorf("expected 'variational', got %q", c.Name())
	}
}

func TestFetchTrades(t *testing.T) {
	account := testAccount()
	accountID, _ := uuid.Parse(account.ID)

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if aid != accountID {
				t.Errorf("unexpected account ID: %s", aid)
			}
			if eventType != "trade" {
				t.Errorf("expected event_type 'trade', got %q", eventType)
			}
			return []*db.OmniRawEvent{
				makeTradeEvent("trade-1", "SUPER", "buy", "0.2053", "50000", 1735674516753),
				makeTradeEvent("trade-2", "ANIME", "sell", "0.05", "100000", 1735674600000),
			}, nil
		},
	}

	c := NewClient(mock)
	trades, prices, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices != nil {
		t.Error("expected nil prices")
	}
	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}

	// Verify first trade
	tr := trades[0]
	if tr.TradeID != "trade-1" {
		t.Errorf("expected TradeID 'trade-1', got %q", tr.TradeID)
	}
	if tr.BaseAsset != "SUPER" {
		t.Errorf("expected BaseAsset 'SUPER', got %q", tr.BaseAsset)
	}
	if tr.QuoteAsset != "USDC" {
		t.Errorf("expected QuoteAsset 'USDC', got %q", tr.QuoteAsset)
	}
	if tr.Side != "buy" {
		t.Errorf("expected Side 'buy', got %q", tr.Side)
	}
	if tr.Price != "0.2053" {
		t.Errorf("expected Price '0.2053', got %q", tr.Price)
	}
	if tr.Quantity != "50000" {
		t.Errorf("expected Quantity '50000', got %q", tr.Quantity)
	}
	if tr.Fee != "0" {
		t.Errorf("expected Fee '0', got %q", tr.Fee)
	}
	if tr.FeeAsset != "USDC" {
		t.Errorf("expected FeeAsset 'USDC', got %q", tr.FeeAsset)
	}
	if tr.MarketType != "perp" {
		t.Errorf("expected MarketType 'perp', got %q", tr.MarketType)
	}
	if tr.ExchangeAccountID != accountID {
		t.Errorf("expected ExchangeAccountID %s, got %s", accountID, tr.ExchangeAccountID)
	}
}

func TestFetchTrades_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewClient(&mockDBClient{})
	_, _, err := c.FetchTrades(ctx, testAccount(), time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestFetchTrades_SinceFilter(t *testing.T) {
	account := testAccount()
	since := time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC)

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if sinceMs != since.UnixMilli() {
				t.Errorf("expected sinceMs %d, got %d", since.UnixMilli(), sinceMs)
			}
			return nil, nil
		},
	}

	c := NewClient(mock)
	_, _, err := c.FetchTrades(context.Background(), account, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchDeposits(t *testing.T) {
	account := testAccount()

	mock := &mockDBClient{
		listOmniRawEventsByTypesFunc: func(ctx context.Context, aid uuid.UUID, eventTypes []string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			// Verify we're querying the right event types
			if len(eventTypes) != 4 {
				t.Errorf("expected 4 event types, got %d", len(eventTypes))
			}
			rewardEv := makeTransferEvent("rwd-1", "reward", "257.516005", "USDC", 1731390000000)
			rewardEv.TransferType = strPtr("Referral Reward")
			return []*db.OmniRawEvent{
				makeTransferEvent("dep-1", "deposit", "40000.00002", "USDC", 1731376912604),
				makeTransferEvent("fee-1", "fee", "-0.1", "USDC", 1731376912604),
				makeTransferEvent("wd-1", "withdrawal", "-5000", "USDC", 1731380000000),
				rewardEv,
			}, nil
		},
	}

	c := NewClient(mock)
	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices != nil {
		t.Error("expected nil prices")
	}
	if len(transfers) != 4 {
		t.Fatalf("expected 4 transfers, got %d", len(transfers))
	}

	// Deposit
	dep := transfers[0]
	if dep.Type != models.TypeDeposit {
		t.Errorf("expected type %q, got %q", models.TypeDeposit, dep.Type)
	}
	if dep.Amount != "40000.00002" {
		t.Errorf("expected amount '40000.00002', got %q", dep.Amount)
	}
	if dep.Asset != "USDC" {
		t.Errorf("expected asset 'USDC', got %q", dep.Asset)
	}

	// Fee (negative qty should become positive)
	fee := transfers[1]
	if fee.Type != models.TypeFee {
		t.Errorf("expected type %q, got %q", models.TypeFee, fee.Type)
	}
	if fee.Amount != "0.1" {
		t.Errorf("expected amount '0.1', got %q", fee.Amount)
	}
	if fee.Metadata["fee_type"] != "deposit" {
		t.Errorf("expected fee_type metadata 'deposit', got %q", fee.Metadata["fee_type"])
	}

	// Withdrawal (negative qty should become positive)
	wd := transfers[2]
	if wd.Type != models.TypeWithdraw {
		t.Errorf("expected type %q, got %q", models.TypeWithdraw, wd.Type)
	}
	if wd.Amount != "5000" {
		t.Errorf("expected amount '5000', got %q", wd.Amount)
	}

	// Reward (positive USDC inflow -> reward_pnl)
	rwd := transfers[3]
	if rwd.Type != models.TypeReward {
		t.Errorf("expected type %q, got %q", models.TypeReward, rwd.Type)
	}
	if rwd.Amount != "257.516005" {
		t.Errorf("expected amount '257.516005', got %q", rwd.Amount)
	}
	if rwd.Asset != "USDC" {
		t.Errorf("expected asset 'USDC', got %q", rwd.Asset)
	}
	if rwd.Metadata["reward_type"] != "Referral Reward" {
		t.Errorf("expected reward_type metadata 'Referral Reward', got %q", rwd.Metadata["reward_type"])
	}
}

func TestFetchFundingPayments(t *testing.T) {
	account := testAccount()

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if eventType != "funding" {
				t.Errorf("expected event_type 'funding', got %q", eventType)
			}
			return []*db.OmniRawEvent{
				makeFundingEvent("fund-1", "SUPER", "0.87675", "0.0000125", 1735704000000),
			}, nil
		},
	}

	c := NewClient(mock)
	payments, err := c.FetchFundingPayments(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(payments))
	}

	p := payments[0]
	if p.Type != models.TypeFunding {
		t.Errorf("expected type %q, got %q", models.TypeFunding, p.Type)
	}
	if p.Asset != "USDC" {
		t.Errorf("expected asset 'USDC', got %q", p.Asset)
	}
	if p.Amount != "0.87675" {
		t.Errorf("expected amount '0.87675', got %q", p.Amount)
	}
	if p.Metadata["market"] != "SUPER-PERP" {
		t.Errorf("expected market 'SUPER-PERP', got %q", p.Metadata["market"])
	}
	if p.Metadata["payment_id"] != "fund-1" {
		t.Errorf("expected payment_id 'fund-1', got %q", p.Metadata["payment_id"])
	}
	if p.Metadata["funding_rate"] != "0.0000125" {
		t.Errorf("expected funding_rate '0.0000125', got %q", p.Metadata["funding_rate"])
	}
}

func TestFetchSettlements_ReturnsNil(t *testing.T) {
	c := NewClient(&mockDBClient{})
	settlements, err := c.FetchSettlements(context.Background(), testAccount(), time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settlements != nil {
		t.Error("expected nil settlements")
	}
}

func TestFetchBalances_ReturnsNil(t *testing.T) {
	c := NewClient(&mockDBClient{})
	balances, err := c.FetchBalances(context.Background(), testAccount())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if balances != nil {
		t.Error("expected nil balances")
	}
}

func TestDiscoverAccounts_ReturnsNotImplemented(t *testing.T) {
	c := NewClient(&mockDBClient{})
	_, err := c.DiscoverAccounts(context.Background(), "test")
	if !errors.Is(err, iface.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestAbsNumericString(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"100", "100"},
		{"-100", "100"},
		{"-0.1", "0.1"},
		{"0", "0"},
		{"-0", "0"},
		{"123.456", "123.456"},
		{"-123.456", "123.456"},
	}

	for _, tt := range tests {
		result := absNumericString(tt.input)
		if result != tt.expected {
			t.Errorf("absNumericString(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests using real OMNI CSV data
// ---------------------------------------------------------------------------

// TestFetchTradesWithRealData uses OmniRawEvent rows that mirror the real OMNI
// trades CSV export to verify full end-to-end mapping.
func TestFetchTradesWithRealData(t *testing.T) {
	account := testAccount()
	accountID, _ := uuid.Parse(account.ID)

	// Real OMNI trades data (timestamps derived from created_at):
	// d0672381 2025-12-31T19:48:36.753006Z buy  SUPER 0.205300000000 50000
	// 996f4f51 2025-12-30T22:53:33.682535Z buy  SUPER 0.208700000000 50000
	// d516d4b6 2025-12-30T07:11:03.502226Z sell ANIME 0.007594000000 2000000
	// dae6c563 2025-12-28T21:30:14.250804Z buy  TNSR  0.079390000000 50000
	//
	// The DB returns events ordered by timestamp_ms ASC, so:
	// 1. dae6c563 (Dec 28)
	// 2. d516d4b6 (Dec 30 07:11)
	// 3. 996f4f51 (Dec 30 22:53)
	// 4. d0672381 (Dec 31 19:48)

	ts1 := time.Date(2025, 12, 28, 21, 30, 14, 250804000, time.UTC).UnixMilli()
	ts2 := time.Date(2025, 12, 30, 7, 11, 3, 502226000, time.UTC).UnixMilli()
	ts3 := time.Date(2025, 12, 30, 22, 53, 33, 682535000, time.UTC).UnixMilli()
	ts4 := time.Date(2025, 12, 31, 19, 48, 36, 753006000, time.UTC).UnixMilli()

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if aid != accountID {
				t.Errorf("unexpected account ID: %s", aid)
			}
			if eventType != "trade" {
				t.Errorf("expected event_type 'trade', got %q", eventType)
			}
			// Return in ascending timestamp order (as the real DB query does)
			return []*db.OmniRawEvent{
				makeTradeEvent("dae6c563-f008-4c98-970e-949b06d28bc5", "TNSR", "buy", "0.079390000000", "50000.000000000000", ts1),
				makeTradeEvent("d516d4b6-58a2-4f0a-a6eb-517cfe592ee6", "ANIME", "sell", "0.007594000000", "2000000.000000000000", ts2),
				makeTradeEvent("996f4f51-c572-4088-b9d8-358348fe9987", "SUPER", "buy", "0.208700000000", "50000.000000000000", ts3),
				makeTradeEvent("d0672381-d7f6-453c-b887-b055b1ce0419", "SUPER", "buy", "0.205300000000", "50000.000000000000", ts4),
			}, nil
		},
	}

	c := NewClient(mock)
	trades, prices, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices != nil {
		t.Error("expected nil prices")
	}
	if len(trades) != 4 {
		t.Fatalf("expected 4 trades, got %d", len(trades))
	}

	// All trades should be sorted ascending by timestamp (DB guarantees this)
	for i := 1; i < len(trades); i++ {
		if trades[i].Timestamp.Before(trades[i-1].Timestamp) {
			t.Errorf("trades not sorted ascending: trade[%d] %v < trade[%d] %v",
				i, trades[i].Timestamp, i-1, trades[i-1].Timestamp)
		}
	}

	// Trade 0: TNSR buy (earliest)
	tr0 := trades[0]
	if tr0.TradeID != "dae6c563-f008-4c98-970e-949b06d28bc5" {
		t.Errorf("trade[0] TradeID = %q, want dae6c563-...", tr0.TradeID)
	}
	if tr0.BaseAsset != "TNSR" {
		t.Errorf("trade[0] BaseAsset = %q, want TNSR", tr0.BaseAsset)
	}
	if tr0.Side != "buy" {
		t.Errorf("trade[0] Side = %q, want buy", tr0.Side)
	}

	// Trade 1: ANIME sell
	tr1 := trades[1]
	if tr1.TradeID != "d516d4b6-58a2-4f0a-a6eb-517cfe592ee6" {
		t.Errorf("trade[1] TradeID = %q, want d516d4b6-...", tr1.TradeID)
	}
	if tr1.BaseAsset != "ANIME" {
		t.Errorf("trade[1] BaseAsset = %q, want ANIME", tr1.BaseAsset)
	}
	if tr1.Side != "sell" {
		t.Errorf("trade[1] Side = %q, want sell", tr1.Side)
	}
	if tr1.Quantity != "2000000.000000000000" {
		t.Errorf("trade[1] Quantity = %q, want 2000000.000000000000", tr1.Quantity)
	}

	// Trade 3: SUPER buy (latest)
	tr3 := trades[3]
	if tr3.TradeID != "d0672381-d7f6-453c-b887-b055b1ce0419" {
		t.Errorf("trade[3] TradeID = %q, want d0672381-...", tr3.TradeID)
	}
	if tr3.BaseAsset != "SUPER" {
		t.Errorf("trade[3] BaseAsset = %q, want SUPER", tr3.BaseAsset)
	}
	if tr3.QuoteAsset != "USDC" {
		t.Errorf("trade[3] QuoteAsset = %q, want USDC", tr3.QuoteAsset)
	}
	if tr3.Side != "buy" {
		t.Errorf("trade[3] Side = %q, want buy", tr3.Side)
	}
	if tr3.Price != "0.205300000000" {
		t.Errorf("trade[3] Price = %q, want 0.205300000000", tr3.Price)
	}
	if tr3.Quantity != "50000.000000000000" {
		t.Errorf("trade[3] Quantity = %q, want 50000.000000000000", tr3.Quantity)
	}
	if tr3.Fee != "0" {
		t.Errorf("trade[3] Fee = %q, want 0", tr3.Fee)
	}
	if tr3.MarketType != "perp" {
		t.Errorf("trade[3] MarketType = %q, want perp", tr3.MarketType)
	}
}

// TestFetchDepositsWithRealData uses real OMNI transfer CSV rows covering
// deposit, withdrawal, and fee event types.
func TestFetchDepositsWithRealData(t *testing.T) {
	account := testAccount()
	accountID, _ := uuid.Parse(account.ID)

	// Real OMNI transfers:
	// c2ae13c7 2025-12-01T07:42:27.968760Z  12184.850000000000 USDC deposit
	// d106db26 2025-12-01T07:42:27.968760Z  -0.100000000000    USDC fee (deposit)
	// 1f467f3d 2025-12-03T17:32:49.117549Z  -25000.000000000000 USDC withdrawal
	// 5133f294 2025-12-03T17:32:49.117549Z  -0.100000000000    USDC fee (withdrawal)
	// acbe27fc 2025-12-03T21:14:31.146533Z  -0.100000000000    USDC fee (deposit)

	ts1 := time.Date(2025, 12, 1, 7, 42, 27, 968760000, time.UTC).UnixMilli()
	ts2 := time.Date(2025, 12, 3, 17, 32, 49, 117549000, time.UTC).UnixMilli()
	ts3 := time.Date(2025, 12, 3, 21, 14, 31, 146533000, time.UTC).UnixMilli()

	depositFee := makeTransferEvent("d106db26-89e4-4376-8626-1a6d0d161ae2", "fee", "-0.100000000000", "USDC", ts1)
	depositFee.FeeType = strPtr("deposit")

	withdrawalFee := makeTransferEvent("5133f294-5243-419f-b3c8-320992e76fd8", "fee", "-0.100000000000", "USDC", ts2)
	withdrawalFee.FeeType = strPtr("withdrawal")

	depositFee2 := makeTransferEvent("acbe27fc-887f-40a1-8930-dd5171ab1413", "fee", "-0.100000000000", "USDC", ts3)
	depositFee2.FeeType = strPtr("deposit")

	mock := &mockDBClient{
		listOmniRawEventsByTypesFunc: func(ctx context.Context, aid uuid.UUID, eventTypes []string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if aid != accountID {
				t.Errorf("unexpected account ID: %s", aid)
			}
			return []*db.OmniRawEvent{
				makeTransferEvent("c2ae13c7-3dd9-49f2-8b48-3bc2336fb59f", "deposit", "12184.850000000000", "USDC", ts1),
				depositFee,
				makeTransferEvent("1f467f3d-6b3b-4181-a10d-5636bc662e24", "withdrawal", "-25000.000000000000", "USDC", ts2),
				withdrawalFee,
				depositFee2,
			}, nil
		},
	}

	c := NewClient(mock)
	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prices != nil {
		t.Error("expected nil prices")
	}
	if len(transfers) != 5 {
		t.Fatalf("expected 5 transfers, got %d", len(transfers))
	}

	// Transfer 0: Deposit — amount should be positive as-is
	dep := transfers[0]
	if dep.Type != models.TypeDeposit {
		t.Errorf("transfer[0] Type = %q, want %q", dep.Type, models.TypeDeposit)
	}
	if dep.Asset != "USDC" {
		t.Errorf("transfer[0] Asset = %q, want USDC", dep.Asset)
	}
	if dep.Amount != "12184.850000000000" {
		t.Errorf("transfer[0] Amount = %q, want 12184.850000000000", dep.Amount)
	}
	if dep.Metadata["payment_id"] != "c2ae13c7-3dd9-49f2-8b48-3bc2336fb59f" {
		t.Errorf("transfer[0] payment_id = %q, want c2ae13c7-...", dep.Metadata["payment_id"])
	}

	// Transfer 1: Fee on deposit — negative qty becomes positive
	fee1 := transfers[1]
	if fee1.Type != models.TypeFee {
		t.Errorf("transfer[1] Type = %q, want %q", fee1.Type, models.TypeFee)
	}
	if fee1.Amount != "0.1" {
		t.Errorf("transfer[1] Amount = %q, want 0.1", fee1.Amount)
	}
	if fee1.Metadata["fee_type"] != "deposit" {
		t.Errorf("transfer[1] fee_type = %q, want deposit", fee1.Metadata["fee_type"])
	}

	// Transfer 2: Withdrawal — negative qty becomes positive
	wd := transfers[2]
	if wd.Type != models.TypeWithdraw {
		t.Errorf("transfer[2] Type = %q, want %q", wd.Type, models.TypeWithdraw)
	}
	if wd.Amount != "25000" {
		t.Errorf("transfer[2] Amount = %q, want 25000", wd.Amount)
	}

	// Transfer 3: Fee on withdrawal — negative qty becomes positive
	fee2 := transfers[3]
	if fee2.Type != models.TypeFee {
		t.Errorf("transfer[3] Type = %q, want %q", fee2.Type, models.TypeFee)
	}
	if fee2.Amount != "0.1" {
		t.Errorf("transfer[3] Amount = %q, want 0.1", fee2.Amount)
	}
	if fee2.Metadata["fee_type"] != "withdrawal" {
		t.Errorf("transfer[3] fee_type = %q, want withdrawal", fee2.Metadata["fee_type"])
	}
}

// TestFetchFundingWithRealData uses real OMNI funding CSV rows.
func TestFetchFundingWithRealData(t *testing.T) {
	account := testAccount()
	accountID, _ := uuid.Parse(account.ID)

	// Real OMNI funding data:
	// 31a7c969 2026-01-01T05:00:00Z 0.876750000000 USDC funding SUPER 0.0000125
	// 7d6df261 2026-01-01T05:00:00Z 0.195125000000 USDC funding TNSR  0.0000125
	// dcc69971 2026-01-01T05:00:00Z 0.179573000000 USDC funding ANIME 0.00001249988584474886
	// 893c3f91 2026-01-01T06:00:00Z 0.880250000000 USDC funding SUPER 0.0000125

	ts5am := time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC).UnixMilli()
	ts6am := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC).UnixMilli()

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			if aid != accountID {
				t.Errorf("unexpected account ID: %s", aid)
			}
			if eventType != "funding" {
				t.Errorf("expected event_type 'funding', got %q", eventType)
			}
			return []*db.OmniRawEvent{
				makeFundingEvent("31a7c969-1a96-4df2-85ba-eb85d2cfb85b", "SUPER", "0.876750000000", "0.0000125", ts5am),
				makeFundingEvent("7d6df261-6ef5-4152-8413-a679e3b66f9b", "TNSR", "0.195125000000", "0.0000125", ts5am),
				makeFundingEvent("dcc69971-c8e5-45cd-94eb-cf3be23a8e54", "ANIME", "0.179573000000", "0.00001249988584474886", ts5am),
				makeFundingEvent("893c3f91-0c71-457e-97da-c4f0bcde5ddd", "SUPER", "0.880250000000", "0.0000125", ts6am),
			}, nil
		},
	}

	c := NewClient(mock)
	payments, err := c.FetchFundingPayments(context.Background(), account, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payments) != 4 {
		t.Fatalf("expected 4 funding payments, got %d", len(payments))
	}

	// Payment 0: SUPER funding at 05:00
	p0 := payments[0]
	if p0.Type != models.TypeFunding {
		t.Errorf("payment[0] Type = %q, want %q", p0.Type, models.TypeFunding)
	}
	if p0.Asset != "USDC" {
		t.Errorf("payment[0] Asset = %q, want USDC", p0.Asset)
	}
	if p0.Amount != "0.876750000000" {
		t.Errorf("payment[0] Amount = %q, want 0.876750000000", p0.Amount)
	}
	if p0.Metadata["market"] != "SUPER-PERP" {
		t.Errorf("payment[0] market = %q, want SUPER-PERP", p0.Metadata["market"])
	}
	if p0.Metadata["funding_rate"] != "0.0000125" {
		t.Errorf("payment[0] funding_rate = %q, want 0.0000125", p0.Metadata["funding_rate"])
	}
	if p0.Metadata["payment_id"] != "31a7c969-1a96-4df2-85ba-eb85d2cfb85b" {
		t.Errorf("payment[0] payment_id = %q, want 31a7c969-...", p0.Metadata["payment_id"])
	}

	// Payment 1: TNSR funding
	p1 := payments[1]
	if p1.Amount != "0.195125000000" {
		t.Errorf("payment[1] Amount = %q, want 0.195125000000", p1.Amount)
	}
	if p1.Metadata["market"] != "TNSR-PERP" {
		t.Errorf("payment[1] market = %q, want TNSR-PERP", p1.Metadata["market"])
	}

	// Payment 2: ANIME funding with long-precision funding rate
	p2 := payments[2]
	if p2.Amount != "0.179573000000" {
		t.Errorf("payment[2] Amount = %q, want 0.179573000000", p2.Amount)
	}
	if p2.Metadata["market"] != "ANIME-PERP" {
		t.Errorf("payment[2] market = %q, want ANIME-PERP", p2.Metadata["market"])
	}
	if p2.Metadata["funding_rate"] != "0.00001249988584474886" {
		t.Errorf("payment[2] funding_rate = %q, want 0.00001249988584474886", p2.Metadata["funding_rate"])
	}

	// Payment 3: SUPER funding at 06:00 (different hour)
	p3 := payments[3]
	if p3.Amount != "0.880250000000" {
		t.Errorf("payment[3] Amount = %q, want 0.880250000000", p3.Amount)
	}
	if p3.Metadata["market"] != "SUPER-PERP" {
		t.Errorf("payment[3] market = %q, want SUPER-PERP", p3.Metadata["market"])
	}
	// Verify timestamps differ between 5am and 6am entries
	if payments[0].Timestamp.Equal(payments[3].Timestamp) {
		t.Error("payment[0] and payment[3] should have different timestamps (05:00 vs 06:00)")
	}
}

// TestFetchTrades_DBError verifies error propagation from the DB client.
func TestFetchTrades_DBError(t *testing.T) {
	account := testAccount()
	expectedErr := errors.New("db connection failed")

	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			return nil, expectedErr
		},
	}

	c := NewClient(mock)
	_, _, err := c.FetchTrades(context.Background(), account, time.Time{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected wrapped db error, got %v", err)
	}
}

// TestFetchTrades_EmptyResult verifies empty DB results produce empty slices.
func TestFetchTrades_EmptyResult(t *testing.T) {
	mock := &mockDBClient{
		listOmniRawEventsFunc: func(ctx context.Context, aid uuid.UUID, eventType string, sinceMs int64) ([]*db.OmniRawEvent, error) {
			return []*db.OmniRawEvent{}, nil
		},
	}

	c := NewClient(mock)
	trades, _, err := c.FetchTrades(context.Background(), testAccount(), time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != 0 {
		t.Errorf("expected 0 trades, got %d", len(trades))
	}
}

// Verify interface compliance at compile time.
var _ iface.ExchangeClient = (*Client)(nil)
