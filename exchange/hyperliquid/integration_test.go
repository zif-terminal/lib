// +build integration

package hyperliquid

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// TestHyperliquidClient_Integration tests against real Hyperliquid API
// Set HYPERLIQUID_TEST_ADDRESS environment variable to run
func TestHyperliquidClient_Integration(t *testing.T) {
	testAddress := os.Getenv("HYPERLIQUID_TEST_ADDRESS")
	if testAddress == "" {
		t.Skip("Skipping integration test: HYPERLIQUID_TEST_ADDRESS not set")
	}

	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch trades
	trades, err := client.FetchTrades(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("Failed to fetch trades: %v", err)
	}

	t.Logf("Fetched %d trades", len(trades))

	// Verify trade structure
	for i, trade := range trades {
		if trade.TradeID == "" {
			t.Errorf("Trade %d: TradeID is empty", i)
		}
		if trade.Side != "buy" && trade.Side != "sell" {
			t.Errorf("Trade %d: Side must be 'buy' or 'sell', got '%s'", i, trade.Side)
		}
		if trade.BaseAsset == "" {
			t.Errorf("Trade %d: BaseAsset is empty", i)
		}
		if trade.QuoteAsset == "" {
			t.Errorf("Trade %d: QuoteAsset is empty", i)
		}
		if trade.Price == "" {
			t.Errorf("Trade %d: Price is empty", i)
		}
		if trade.Quantity == "" {
			t.Errorf("Trade %d: Quantity is empty", i)
		}
		if trade.Timestamp.IsZero() {
			t.Errorf("Trade %d: Timestamp is zero", i)
		}
	}

	// Verify sorting (oldest first)
	for i := 1; i < len(trades); i++ {
		if trades[i].Timestamp.Before(trades[i-1].Timestamp) {
			t.Errorf("Trades not sorted: trade %d (%v) is before trade %d (%v)",
				i, trades[i].Timestamp, i-1, trades[i-1].Timestamp)
		}
	}
}

// TestHyperliquidClient_Integration_FilterBySince tests filtering by timestamp
func TestHyperliquidClient_Integration_FilterBySince(t *testing.T) {
	testAddress := os.Getenv("HYPERLIQUID_TEST_ADDRESS")
	if testAddress == "" {
		t.Skip("Skipping integration test: HYPERLIQUID_TEST_ADDRESS not set")
	}

	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch all trades
	allTrades, err := client.FetchTrades(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("Failed to fetch all trades: %v", err)
	}

	if len(allTrades) == 0 {
		t.Skip("Skipping filter test: no trades available")
	}

	// Use a timestamp in the middle
	middleIdx := len(allTrades) / 2
	since := allTrades[middleIdx].Timestamp

	// Fetch trades since that timestamp
	filteredTrades, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("Failed to fetch filtered trades: %v", err)
	}

	// All filtered trades should be >= since
	for i, trade := range filteredTrades {
		if trade.Timestamp.Before(since) {
			t.Errorf("Filtered trade %d timestamp %v is before since %v",
				i, trade.Timestamp, since)
		}
	}

	// Filtered trades should be <= all trades
	if len(filteredTrades) > len(allTrades) {
		t.Errorf("Filtered trades (%d) should not exceed all trades (%d)",
			len(filteredTrades), len(allTrades))
	}

	t.Logf("All trades: %d, Filtered trades: %d", len(allTrades), len(filteredTrades))
}

// TestHyperliquidClient_Integration_Contract runs contract tests against real API
func TestHyperliquidClient_Integration_Contract(t *testing.T) {
	testAddress := os.Getenv("HYPERLIQUID_TEST_ADDRESS")
	if testAddress == "" {
		t.Skip("Skipping integration test: HYPERLIQUID_TEST_ADDRESS not set")
	}

	validAccount := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testAddress,
	}

	invalidAccount := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0xinvalid", // Malformed address that will cause API error
	}

	contract := iface.ExchangeClientContract{
		NewClient: func() iface.ExchangeClient {
			return NewClient()
		},
		ValidAccount:   validAccount,
		InvalidAccount: invalidAccount,
	}

	// Run contract tests
	iface.RunExchangeClientContractTests(t, contract)
}

// TestHyperliquidClient_Integration_FundingPayments tests FetchFundingPayments against real Hyperliquid API
// Set HYPERLIQUID_TEST_ADDRESS environment variable to run
func TestHyperliquidClient_Integration_FundingPayments(t *testing.T) {
	testAddress := os.Getenv("HYPERLIQUID_TEST_ADDRESS")
	if testAddress == "" {
		t.Skip("Skipping integration test: HYPERLIQUID_TEST_ADDRESS not set")
	}

	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch funding payments
	payments, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("Failed to fetch funding payments: %v", err)
	}

	t.Logf("Fetched %d funding payments", len(payments))

	// Verify payment structure
	for i, payment := range payments {
		if payment.PaymentID == "" {
			t.Errorf("Payment %d: PaymentID is empty", i)
		}
		if payment.BaseAsset == "" {
			t.Errorf("Payment %d: BaseAsset is empty", i)
		}
		if payment.QuoteAsset == "" {
			t.Errorf("Payment %d: QuoteAsset is empty", i)
		}
		if payment.Amount == "" {
			t.Errorf("Payment %d: Amount is empty", i)
		}
		if payment.Timestamp.IsZero() {
			t.Errorf("Payment %d: Timestamp is zero", i)
		}
		if payment.ExchangeAccountID == uuid.Nil {
			t.Errorf("Payment %d: ExchangeAccountID is zero", i)
		}
	}

	// Verify sorting (oldest first)
	for i := 1; i < len(payments); i++ {
		if payments[i].Timestamp.Before(payments[i-1].Timestamp) {
			t.Errorf("Payments not sorted: payment %d (%v) is before payment %d (%v)",
				i, payments[i].Timestamp, i-1, payments[i-1].Timestamp)
		}
	}
}

// TestHyperliquidClient_Integration_FundingPayments_FilterBySince tests filtering funding payments by timestamp
func TestHyperliquidClient_Integration_FundingPayments_FilterBySince(t *testing.T) {
	testAddress := os.Getenv("HYPERLIQUID_TEST_ADDRESS")
	if testAddress == "" {
		t.Skip("Skipping integration test: HYPERLIQUID_TEST_ADDRESS not set")
	}

	client := NewClient()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testAddress,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch all payments
	allPayments, err := client.FetchFundingPayments(ctx, account, time.Time{})
	if err != nil {
		t.Fatalf("Failed to fetch all payments: %v", err)
	}

	if len(allPayments) == 0 {
		t.Skip("Skipping filter test: no payments available")
	}

	// Use a timestamp in the middle
	middleIdx := len(allPayments) / 2
	since := allPayments[middleIdx].Timestamp

	// Fetch payments since that timestamp
	filteredPayments, err := client.FetchFundingPayments(ctx, account, since)
	if err != nil {
		t.Fatalf("Failed to fetch filtered payments: %v", err)
	}

	// All filtered payments should be >= since
	for i, payment := range filteredPayments {
		if payment.Timestamp.Before(since) {
			t.Errorf("Filtered payment %d timestamp %v is before since %v",
				i, payment.Timestamp, since)
		}
	}

	// Filtered payments should be <= all payments
	if len(filteredPayments) > len(allPayments) {
		t.Errorf("Filtered payments (%d) should not exceed all payments (%d)",
			len(filteredPayments), len(allPayments))
	}

	t.Logf("All payments: %d, Filtered payments: %d", len(allPayments), len(filteredPayments))
}
