//go:build integration

package hyperliquid

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

const testWallet = "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

func testAccount() *models.ExchangeAccount {
	return &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: testWallet,
	}
}

func TestIntegration_DiscoverAccounts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient()
	accounts, err := client.DiscoverAccounts(ctx, testWallet)
	if err != nil {
		t.Fatalf("DiscoverAccounts failed: %v", err)
	}

	fmt.Printf("\n=== DiscoverAccounts ===\n")
	fmt.Printf("Accounts found: %d\n", len(accounts))
	for i, a := range accounts {
		fmt.Printf("  [%d] Identifier=%s Type=%s Name=%s Metadata=%v\n", i, a.AccountIdentifier, a.AccountType, a.Name, a.Metadata)
	}

	if len(accounts) == 0 {
		t.Fatal("Expected at least one account to be discovered")
	}
}

func TestIntegration_FetchTrades(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client := NewClient()
	account := testAccount()

	since := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	trades, prices, err := client.FetchTrades(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchTrades failed: %v", err)
	}

	fmt.Printf("\n=== FetchTrades ===\n")
	fmt.Printf("Total trades: %d\n", len(trades))
	fmt.Printf("Price records: %v\n", prices)

	if len(trades) == 0 {
		t.Fatal("Expected at least one trade")
	}

	// Date range
	var earliest, latest time.Time
	earliest = trades[0].Timestamp
	latest = trades[0].Timestamp
	for _, tr := range trades {
		if tr.Timestamp.Before(earliest) {
			earliest = tr.Timestamp
		}
		if tr.Timestamp.After(latest) {
			latest = tr.Timestamp
		}
	}
	fmt.Printf("Date range: %s to %s\n", earliest.Format("2006-01-02"), latest.Format("2006-01-02"))

	// Market breakdown
	markets := map[string]int{}
	spotCount, perpCount := 0, 0
	for _, tr := range trades {
		key := tr.BaseAsset + "/" + tr.QuoteAsset + " (" + tr.MarketType + ")"
		markets[key]++
		if tr.MarketType == "spot" {
			spotCount++
		} else {
			perpCount++
		}
	}
	fmt.Printf("Perp trades: %d, Spot trades: %d\n", perpCount, spotCount)

	// Sort markets by count descending
	type kv struct {
		Key   string
		Count int
	}
	var sorted []kv
	for k, v := range markets {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })
	fmt.Printf("Markets (top 20):\n")
	for i, kv := range sorted {
		if i >= 20 {
			break
		}
		fmt.Printf("  %s: %d\n", kv.Key, kv.Count)
	}

	// Show sample trades
	fmt.Printf("\nSample trades (first 5):\n")
	for i, tr := range trades {
		if i >= 5 {
			break
		}
		fmt.Printf("  [%d] %s %s %s/%s price=%s qty=%s fee=%s type=%s tradeID=%s\n",
			i, tr.Timestamp.Format("2006-01-02 15:04:05"), tr.Side, tr.BaseAsset, tr.QuoteAsset,
			tr.Price, tr.Quantity, tr.Fee, tr.MarketType, tr.TradeID)
	}
	fmt.Printf("\nSample trades (last 5):\n")
	start := len(trades) - 5
	if start < 0 {
		start = 0
	}
	for i := start; i < len(trades); i++ {
		tr := trades[i]
		fmt.Printf("  [%d] %s %s %s/%s price=%s qty=%s fee=%s type=%s tradeID=%s\n",
			i, tr.Timestamp.Format("2006-01-02 15:04:05"), tr.Side, tr.BaseAsset, tr.QuoteAsset,
			tr.Price, tr.Quantity, tr.Fee, tr.MarketType, tr.TradeID)
	}

	// Pagination check
	if len(trades) > 2000 {
		fmt.Printf("\nPAGINATION VERIFIED: %d trades fetched (>2000, so multiple pages were used)\n", len(trades))
	} else {
		fmt.Printf("\nPagination: Only %d trades (<= 2000), single page sufficient\n", len(trades))
	}

	// Verify sorted ascending
	for i := 1; i < len(trades); i++ {
		if trades[i].Timestamp.Before(trades[i-1].Timestamp) {
			t.Errorf("Trades not sorted ascending at index %d: %v < %v", i, trades[i].Timestamp, trades[i-1].Timestamp)
			break
		}
	}

	// Verify all quote assets are USDC
	for _, tr := range trades {
		if tr.QuoteAsset != "USDC" {
			t.Errorf("Expected quote asset USDC, got %s for trade %s", tr.QuoteAsset, tr.TradeID)
			break
		}
	}

	// Verify sides are valid
	for _, tr := range trades {
		if tr.Side != "buy" && tr.Side != "sell" {
			t.Errorf("Invalid side %q for trade %s", tr.Side, tr.TradeID)
			break
		}
	}
}

func TestIntegration_FetchFundingPayments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client := NewClient()
	account := testAccount()

	since := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	payments, err := client.FetchFundingPayments(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchFundingPayments failed: %v", err)
	}

	fmt.Printf("\n=== FetchFundingPayments ===\n")
	fmt.Printf("Total funding payments: %d\n", len(payments))

	if len(payments) == 0 {
		fmt.Println("No funding payments found (wallet may have no perp activity)")
		return
	}

	// Date range
	earliest := payments[0].Timestamp
	latest := payments[len(payments)-1].Timestamp
	fmt.Printf("Date range: %s to %s\n", earliest.Format("2006-01-02"), latest.Format("2006-01-02"))

	// Check n_samples metadata
	nSamplesCounts := map[string]int{}
	for _, p := range payments {
		ns := p.Metadata["n_samples"]
		nSamplesCounts[ns]++
	}
	fmt.Printf("n_samples distribution:\n")
	for ns, count := range nSamplesCounts {
		fmt.Printf("  n_samples=%s: %d payments\n", ns, count)
	}

	// Check markets
	marketCounts := map[string]int{}
	for _, p := range payments {
		marketCounts[p.Metadata["market"]]++
	}
	fmt.Printf("Markets in funding:\n")
	var fundingSorted []struct {
		Market string
		Count  int
	}
	for m, c := range marketCounts {
		fundingSorted = append(fundingSorted, struct {
			Market string
			Count  int
		}{m, c})
	}
	sort.Slice(fundingSorted, func(i, j int) bool { return fundingSorted[i].Count > fundingSorted[j].Count })
	for i, kv := range fundingSorted {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s: %d\n", kv.Market, kv.Count)
	}

	// Sample entries
	fmt.Printf("\nSample funding (first 5):\n")
	for i, p := range payments {
		if i >= 5 {
			break
		}
		fmt.Printf("  [%d] %s asset=%s amount=%s type=%s metadata=%v\n",
			i, p.Timestamp.Format("2006-01-02 15:04:05"), p.Asset, p.Amount, p.Type, p.Metadata)
	}

	// Verify all are funding type
	for _, p := range payments {
		if p.Type != "funding" {
			t.Errorf("Expected type 'funding', got %q", p.Type)
			break
		}
	}

	// Verify sorted ascending
	for i := 1; i < len(payments); i++ {
		if payments[i].Timestamp.Before(payments[i-1].Timestamp) {
			t.Errorf("Funding not sorted ascending at index %d", i)
			break
		}
	}
}

func TestIntegration_FetchDeposits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := NewClient()
	account := testAccount()

	since := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	transfers, prices, err := client.FetchDeposits(ctx, account, since)
	if err != nil {
		t.Fatalf("FetchDeposits failed: %v", err)
	}

	fmt.Printf("\n=== FetchDeposits ===\n")
	fmt.Printf("Total transfers: %d\n", len(transfers))
	fmt.Printf("Price records: %v\n", prices)

	if len(transfers) == 0 {
		fmt.Println("No deposits/withdrawals found")
		return
	}

	// Type breakdown
	typeCounts := map[string]int{}
	for _, tr := range transfers {
		typeCounts[tr.Type]++
	}
	fmt.Printf("Types:\n")
	for typ, count := range typeCounts {
		fmt.Printf("  %s: %d\n", typ, count)
	}

	// Date range
	earliest := transfers[0].Timestamp
	latest := transfers[len(transfers)-1].Timestamp
	fmt.Printf("Date range: %s to %s\n", earliest.Format("2006-01-02"), latest.Format("2006-01-02"))

	// Show all transfers
	fmt.Printf("\nAll transfers:\n")
	for i, tr := range transfers {
		fmt.Printf("  [%d] %s type=%s asset=%s amount=%s\n",
			i, tr.Timestamp.Format("2006-01-02 15:04:05"), tr.Type, tr.Asset, tr.Amount)
	}
}

func TestIntegration_FetchBalances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewClient()
	account := testAccount()

	balances, err := client.FetchBalances(ctx, account)
	if err != nil {
		t.Fatalf("FetchBalances failed: %v", err)
	}

	fmt.Printf("\n=== FetchBalances ===\n")
	fmt.Printf("Total balance entries: %d\n", len(balances))

	for _, b := range balances {
		fmt.Printf("  Asset=%s Balance=%.6f TimestampMs=%d\n", b.Asset, b.Balance, b.TimestampMs)
	}
}

func TestIntegration_PaginationVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client := NewClient()

	// Fetch all fills directly using the internal method to check pagination behavior
	since := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	fills, err := client.fetchAllFills(ctx, testWallet, since)
	if err != nil {
		t.Fatalf("fetchAllFills failed: %v", err)
	}

	fmt.Printf("\n=== Pagination Verification ===\n")
	fmt.Printf("Total fills fetched: %d\n", len(fills))

	if len(fills) > maxFillsPerPage {
		fmt.Printf("PAGINATION CONFIRMED: %d fills > %d per page limit\n", len(fills), maxFillsPerPage)

		// Verify no duplicate trade IDs
		seen := map[int64]bool{}
		dupes := 0
		for _, f := range fills {
			if seen[f.Tid] {
				dupes++
			}
			seen[f.Tid] = true
		}
		fmt.Printf("Unique trade IDs: %d, Duplicates: %d\n", len(seen), dupes)
		if dupes > 0 {
			t.Errorf("Found %d duplicate trade IDs across pages", dupes)
		}
	} else {
		fmt.Printf("Only %d fills, pagination not needed (< %d limit)\n", len(fills), maxFillsPerPage)
	}

	// Check timestamp ordering of raw fills
	outOfOrder := 0
	for i := 1; i < len(fills); i++ {
		if fills[i].Time < fills[i-1].Time {
			outOfOrder++
		}
	}
	fmt.Printf("Raw fills out of order: %d\n", outOfOrder)
}
