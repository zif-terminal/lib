//go:build integration

package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Run with: go test -tags=integration -v ./exchange/lighter/...

func getTestCredentials() (l1Address, authToken string, ok bool) {
	l1Address = os.Getenv("LIGHTER_L1_ADDRESS")
	authToken = os.Getenv("LIGHTER_AUTH_TOKEN")
	if l1Address == "" || authToken == "" {
		return "", "", false
	}
	return l1Address, authToken, true
}

func TestIntegration_DiscoverAccounts(t *testing.T) {
	l1Address, authToken, ok := getTestCredentials()
	if !ok {
		t.Skip("LIGHTER_L1_ADDRESS and LIGHTER_AUTH_TOKEN not set")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	userIdentifier := l1Address + ":" + authToken
	accounts, err := client.DiscoverAccounts(ctx, userIdentifier)
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	fmt.Printf("\n=== Discovered Accounts ===\n")
	fmt.Printf("Found %d account(s)\n\n", len(accounts))

	for i, acc := range accounts {
		fmt.Printf("Account %d:\n", i+1)
		fmt.Printf("  Identifier: %s\n", acc.AccountIdentifier)
		fmt.Printf("  Type: %s\n", acc.AccountType)
		fmt.Printf("  Name: %s\n", acc.Name)
		fmt.Printf("  Metadata: %+v\n\n", acc.Metadata)
	}

	if len(accounts) == 0 {
		t.Error("Expected at least one account")
	}
}

func TestIntegration_FetchTrades(t *testing.T) {
	l1Address, authToken, ok := getTestCredentials()
	if !ok {
		t.Skip("LIGHTER_L1_ADDRESS and LIGHTER_AUTH_TOKEN not set")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Extract account_index from token (format: ro:account_index:...)
	// For this test, we'll use the discovered accounts
	userIdentifier := l1Address + ":" + authToken
	accounts, err := client.DiscoverAccounts(ctx, userIdentifier)
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	if len(accounts) == 0 {
		t.Skip("No accounts discovered")
	}

	// Test FetchTrades for the first account
	acc := accounts[0]
	metadata, _ := json.Marshal(map[string]interface{}{
		"auth_token": authToken,
		"l1_address": l1Address,
	})

	exchangeAccount := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   acc.AccountIdentifier,
		AccountTypeMetadata: metadata,
	}

	// Fetch trades from the last 30 days
	since := time.Now().AddDate(0, 0, -30)
	trades, err := client.FetchTrades(ctx, exchangeAccount, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	fmt.Printf("\n=== Trades (last 30 days) ===\n")
	fmt.Printf("Found %d trade(s) for account %s\n\n", len(trades), acc.AccountIdentifier)

	for i, trade := range trades {
		if i >= 10 {
			fmt.Printf("... and %d more trades\n", len(trades)-10)
			break
		}
		fmt.Printf("Trade %d:\n", i+1)
		fmt.Printf("  ID: %s\n", trade.TradeID)
		fmt.Printf("  Pair: %s/%s\n", trade.BaseAsset, trade.QuoteAsset)
		fmt.Printf("  Side: %s\n", trade.Side)
		fmt.Printf("  Price: %s\n", trade.Price)
		fmt.Printf("  Quantity: %s\n", trade.Quantity)
		fmt.Printf("  Fee: %s\n", trade.Fee)
		fmt.Printf("  Time: %s\n\n", trade.Timestamp.Format(time.RFC3339))
	}
}

func TestIntegration_FetchFundingPayments(t *testing.T) {
	l1Address, authToken, ok := getTestCredentials()
	if !ok {
		t.Skip("LIGHTER_L1_ADDRESS and LIGHTER_AUTH_TOKEN not set")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	userIdentifier := l1Address + ":" + authToken
	accounts, err := client.DiscoverAccounts(ctx, userIdentifier)
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	if len(accounts) == 0 {
		t.Skip("No accounts discovered")
	}

	acc := accounts[0]
	metadata, _ := json.Marshal(map[string]interface{}{
		"auth_token": authToken,
		"l1_address": l1Address,
	})

	exchangeAccount := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   acc.AccountIdentifier,
		AccountTypeMetadata: metadata,
	}

	// Fetch funding payments from the last 30 days
	since := time.Now().AddDate(0, 0, -30)
	payments, err := client.FetchFundingPayments(ctx, exchangeAccount, since)
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	fmt.Printf("\n=== Funding Payments (last 30 days) ===\n")
	fmt.Printf("Found %d payment(s) for account %s\n\n", len(payments), acc.AccountIdentifier)

	for i, payment := range payments {
		if i >= 10 {
			fmt.Printf("... and %d more payments\n", len(payments)-10)
			break
		}
		fmt.Printf("Payment %d:\n", i+1)
		fmt.Printf("  ID: %s\n", payment.Metadata["payment_id"])
		fmt.Printf("  Market: %s\n", payment.Metadata["market"])
		fmt.Printf("  Asset: %s\n", payment.Asset)
		fmt.Printf("  Amount: %s\n", payment.Amount)
		fmt.Printf("  Time: %s\n\n", payment.Timestamp.Format(time.RFC3339))
	}
}

func TestIntegration_FetchAllTrades(t *testing.T) {
	l1Address, authToken, ok := getTestCredentials()
	if !ok {
		t.Skip("LIGHTER_L1_ADDRESS and LIGHTER_AUTH_TOKEN not set")
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	userIdentifier := l1Address + ":" + authToken
	accounts, err := client.DiscoverAccounts(ctx, userIdentifier)
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	if len(accounts) == 0 {
		t.Skip("No accounts discovered")
	}

	acc := accounts[0]
	metadata, _ := json.Marshal(map[string]interface{}{
		"auth_token": authToken,
		"l1_address": l1Address,
	})

	exchangeAccount := &models.ExchangeAccount{
		ID:                  uuid.New().String(),
		AccountIdentifier:   acc.AccountIdentifier,
		AccountTypeMetadata: metadata,
	}

	// Fetch ALL trades (no since filter)
	trades, err := client.FetchTrades(ctx, exchangeAccount, time.Time{})
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	fmt.Printf("\n=== All Trades ===\n")
	fmt.Printf("Found %d total trade(s) for account %s\n", len(trades), acc.AccountIdentifier)

	if len(trades) > 0 {
		fmt.Printf("Oldest: %s\n", trades[0].Timestamp.Format(time.RFC3339))
		fmt.Printf("Newest: %s\n", trades[len(trades)-1].Timestamp.Format(time.RFC3339))
	}
}
