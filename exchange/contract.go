package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zif-terminal/lib/models"
)

// ExchangeClientContract defines the contract tests
// Each exchange implementation should run these tests
type ExchangeClientContract struct {
	NewClient    func() ExchangeClient
	ValidAccount *models.ExchangeAccount
	// InvalidAccount is optional - if nil, invalid account tests are skipped
	InvalidAccount *models.ExchangeAccount
}

// RunExchangeClientContractTests runs all contract tests
// This function is exported so integration tests in other packages can use it
func RunExchangeClientContractTests(t *testing.T, contract ExchangeClientContract) {
	t.Run("Name", func(t *testing.T) {
		client := contract.NewClient()
		name := client.Name()
		if name == "" {
			t.Error("Name() must return non-empty string")
		}
	})

	t.Run("FetchTrades_ValidAccount", func(t *testing.T) {
		client := contract.NewClient()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Should not error with valid account (even if no trades)
		trades, err := client.FetchTrades(ctx, contract.ValidAccount, time.Time{})
		if err != nil {
			t.Errorf("FetchTrades with valid account should not error: %v", err)
		}

		// Verify trade structure if trades returned
		for _, trade := range trades {
			validateTradeInput(t, trade)
		}
	})

	if contract.InvalidAccount != nil {
		t.Run("FetchTrades_InvalidAccount", func(t *testing.T) {
			client := contract.NewClient()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Should error with invalid account
			_, err := client.FetchTrades(ctx, contract.InvalidAccount, time.Time{})
			if err == nil {
				t.Error("FetchTrades with invalid account should error")
			}
		})
	}

	t.Run("FetchTrades_ContextCancellation", func(t *testing.T) {
		client := contract.NewClient()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Should respect context cancellation
		_, err := client.FetchTrades(ctx, contract.ValidAccount, time.Time{})
		if err == nil {
			t.Error("FetchTrades should respect context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("FetchTrades_Timeout", func(t *testing.T) {
		client := contract.NewClient()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Should timeout (or return error quickly)
		_, err := client.FetchTrades(ctx, contract.ValidAccount, time.Time{})
		if err == nil {
			t.Error("FetchTrades should respect timeout")
		}
		// Note: May not always be DeadlineExceeded if HTTP client handles it differently
	})

	t.Run("FetchTrades_SortedByTimestamp", func(t *testing.T) {
		client := contract.NewClient()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		trades, err := client.FetchTrades(ctx, contract.ValidAccount, time.Time{})
		if err != nil {
			t.Skip("Skipping sort test due to error:", err)
		}

		if len(trades) < 2 {
			t.Skip("Skipping sort test: need at least 2 trades")
		}

		// Verify trades are sorted oldest first
		for i := 1; i < len(trades); i++ {
			if trades[i].Timestamp.Before(trades[i-1].Timestamp) {
				t.Error("Trades must be sorted by timestamp (oldest first)")
			}
		}
	})

	t.Run("FetchTrades_FiltersBySince", func(t *testing.T) {
		client := contract.NewClient()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Fetch all trades
		allTrades, err := client.FetchTrades(ctx, contract.ValidAccount, time.Time{})
		if err != nil {
			t.Skip("Skipping filter test due to error:", err)
		}

		if len(allTrades) == 0 {
			t.Skip("Skipping filter test: no trades available")
		}

		// Use a timestamp in the middle
		middleIdx := len(allTrades) / 2
		since := allTrades[middleIdx].Timestamp

		// Fetch trades since that timestamp
		filteredTrades, err := client.FetchTrades(ctx, contract.ValidAccount, since)
		if err != nil {
			t.Fatalf("Failed to fetch filtered trades: %v", err)
		}

		// All filtered trades should be >= since
		for _, trade := range filteredTrades {
			if trade.Timestamp.Before(since) {
				t.Errorf("Trade timestamp %v is before since %v", trade.Timestamp, since)
			}
		}

		// Filtered trades should be <= all trades
		if len(filteredTrades) > len(allTrades) {
			t.Errorf("Filtered trades (%d) should not exceed all trades (%d)", len(filteredTrades), len(allTrades))
		}
	})
}

// validateTradeInput validates TradeInput structure
func validateTradeInput(t *testing.T, trade *models.TradeInput) {
	if trade.TradeID == "" {
		t.Error("TradeInput.TradeID must be non-empty")
	}
	if trade.Side != "buy" && trade.Side != "sell" {
		t.Errorf("TradeInput.Side must be 'buy' or 'sell', got: %s", trade.Side)
	}
	if trade.BaseAsset == "" {
		t.Error("TradeInput.BaseAsset must be non-empty")
	}
	if trade.QuoteAsset == "" {
		t.Error("TradeInput.QuoteAsset must be non-empty")
	}
	if trade.Price == "" {
		t.Error("TradeInput.Price must be non-empty")
	}
	if trade.Quantity == "" {
		t.Error("TradeInput.Quantity must be non-empty")
	}
	if trade.Fee == "" {
		t.Error("TradeInput.Fee must be non-empty")
	}
	if trade.Timestamp.IsZero() {
		t.Error("TradeInput.Timestamp must be non-zero")
	}
}
