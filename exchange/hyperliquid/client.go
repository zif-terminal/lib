package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// Retry configuration for rate limiting
const (
	maxRetries        = 5
	initialBackoff    = 2 * time.Second
	maxBackoff        = 60 * time.Second
	backoffMultiplier = 2.0
)

const baseURL = "https://api.hyperliquid.xyz/info"

// Client implements iface.ExchangeClient for Hyperliquid
type Client struct {
	apiURL     string
	httpClient *http.Client
}

// NewClient creates a new Hyperliquid client
func NewClient() *Client {
	return &Client{
		apiURL:     baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "hyperliquid"
}

// doPost sends a POST request to the Hyperliquid info API and decodes the response.
func (c *Client) doPost(ctx context.Context, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	backoff := initialBackoff
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := globalLimiter.Wait(ctx); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		// Check for rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()

			if attempt == maxRetries {
				return &iface.RateLimitError{
					Exchange:   "hyperliquid",
					Message:    fmt.Sprintf("rate limit exceeded after %d retries", maxRetries),
					RetryAfter: backoff,
				}
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff = time.Duration(float64(backoff) * backoffMultiplier)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
		}

		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("max retries exceeded")
}

// FetchTrades fetches trades from the Hyperliquid userFills API.
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, []*models.PriceRecord, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account ID: %w", err)
	}

	user := account.AccountIdentifier
	if user == "" {
		return nil, nil, fmt.Errorf("account identifier (wallet address) is required")
	}

	fills, err := c.fetchAllFills(ctx, user, since)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch fills: %w", err)
	}

	trades := make([]*models.TradeInput, 0, len(fills))
	for _, fill := range fills {
		trade := transformFill(fill, accountUUID)
		trades = append(trades, trade)
	}

	// Sort by timestamp ascending
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	// Hyperliquid doesn't provide oracle prices in fills
	return trades, nil, nil
}

// FetchFundingPayments fetches funding payments from the Hyperliquid userFunding API.
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	user := account.AccountIdentifier
	if user == "" {
		return nil, fmt.Errorf("account identifier (wallet address) is required")
	}

	entries, err := c.fetchAllFunding(ctx, user, since)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch funding: %w", err)
	}

	payments := make([]*models.TransferInput, 0, len(entries))
	for _, entry := range entries {
		payment := transformFunding(entry, accountUUID)
		payments = append(payments, payment)
	}

	// Sort by timestamp ascending
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Timestamp.Before(payments[j].Timestamp)
	})

	return payments, nil
}

// FetchDeposits fetches deposits and withdrawals from the Hyperliquid ledger API.
func (c *Client) FetchDeposits(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, []*models.PriceRecord, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account ID: %w", err)
	}

	user := account.AccountIdentifier
	if user == "" {
		return nil, nil, fmt.Errorf("account identifier (wallet address) is required")
	}

	entries, err := c.fetchAllLedgerUpdates(ctx, user, since)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch ledger updates: %w", err)
	}

	transfers := make([]*models.TransferInput, 0)
	for _, entry := range entries {
		transfer := transformLedgerEntry(entry, accountUUID)
		if transfer != nil {
			transfers = append(transfers, transfer)
		}
	}

	// Sort by timestamp ascending
	sort.Slice(transfers, func(i, j int) bool {
		return transfers[i].Timestamp.Before(transfers[j].Timestamp)
	})

	// Hyperliquid doesn't provide oracle prices in ledger updates
	return transfers, nil, nil
}

// FetchBalances fetches current spot and perp balances from Hyperliquid.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	user := account.AccountIdentifier
	if user == "" {
		return nil, fmt.Errorf("account identifier (wallet address) is required")
	}

	// Fetch perp clearinghouse state for account value (USDC balance)
	var perpState hlClearinghouseState
	err := c.doPost(ctx, map[string]string{
		"type": "clearinghouseState",
		"user": user,
	}, &perpState)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clearinghouse state: %w", err)
	}

	// Fetch spot balances
	var spotState hlSpotClearinghouseState
	err = c.doPost(ctx, map[string]string{
		"type": "spotClearinghouseState",
		"user": user,
	}, &spotState)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spot clearinghouse state: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	var balances []*models.BalanceSnapshot

	// Add USDC balance from perp account value
	if accountValue, err := strconv.ParseFloat(perpState.MarginSummary.AccountValue, 64); err == nil && math.Abs(accountValue) > 0.000001 {
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       "USDC",
			Balance:     accountValue,
			TimestampMs: nowMs,
		})
	}

	// Add spot balances
	for _, b := range spotState.Balances {
		total, err := strconv.ParseFloat(b.Total, 64)
		if err != nil || math.Abs(total) < 0.000001 {
			continue
		}
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       b.Coin,
			Balance:     total,
			TimestampMs: nowMs,
		})
	}

	return balances, nil
}

// FetchHistoricalBalanceSnapshots returns nil for Hyperliquid (not supported).
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	return nil, nil
}

// FetchSettlements returns nil for Hyperliquid.
// Hyperliquid settles on close (PnL is credited to balance immediately on position close),
// so there are no separate settlement events.
func (c *Client) FetchSettlements(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.Settlement, error) {
	return nil, nil
}

// Transform functions

// transformFill converts a Hyperliquid fill to a TradeInput.
func transformFill(fill hlFill, accountUUID uuid.UUID) *models.TradeInput {
	coin := fill.Coin
	marketType := "perp"
	baseAsset := coin

	// Spot coins have a "-SPOT" suffix in Hyperliquid
	if strings.HasSuffix(coin, "-SPOT") {
		marketType = "spot"
		baseAsset = strings.TrimSuffix(coin, "-SPOT")
	}

	// Canonical spot pairs use "BASE/QUOTE" format (e.g., "PURR/USDC")
	quoteAsset := "USDC"
	if parts := strings.SplitN(baseAsset, "/", 2); len(parts) == 2 {
		marketType = "spot"
		baseAsset = parts[0]
		quoteAsset = parts[1]
	}

	side := "buy"
	if fill.Side == "A" || fill.Side == "S" {
		side = "sell"
	}

	fee := cleanDecimal(fill.Fee)

	return &models.TradeInput{
		TradeID:           strconv.FormatInt(fill.Tid, 10),
		OrderID:           strconv.FormatInt(fill.Oid, 10),
		BaseAsset:         baseAsset,
		QuoteAsset:        quoteAsset,
		Side:              side,
		Price:             cleanDecimal(fill.Px),
		Quantity:          cleanDecimal(fill.Sz),
		Fee:               fee,
		Timestamp:         time.UnixMilli(fill.Time).UTC(),
		ExchangeAccountID: accountUUID,
		MarketType:        marketType,
	}
}

// transformFunding converts a Hyperliquid funding entry to a TransferInput.
func transformFunding(entry hlFundingEntry, accountUUID uuid.UUID) *models.TransferInput {
	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeFunding,
		Asset:             "USDC",
		Amount:            entry.Delta.Usdc, // Keep signed — Drift also stores signed funding amounts
		Timestamp:         time.UnixMilli(entry.Time).UTC(),
		Metadata: map[string]string{
			"market":    entry.Delta.Coin + "-PERP",
			"n_samples": strconv.Itoa(entry.Delta.NSamples),
		},
	}
}

// transformLedgerEntry converts a Hyperliquid ledger entry to a TransferInput.
// Returns nil for unsupported entry types.
func transformLedgerEntry(entry hlLedgerEntry, accountUUID uuid.UUID) *models.TransferInput {
	deltaType := strings.ToLower(entry.Delta.Type)

	var transferType string
	switch deltaType {
	case "deposit":
		transferType = models.TypeDeposit
	case "withdraw":
		transferType = models.TypeWithdraw
	default:
		// Skip unsupported types (internalTransfer, liquidation, etc.)
		return nil
	}

	// Use usdc field; fall back to amount field
	amountStr := entry.Delta.Usdc
	asset := "USDC"
	if amountStr == "" {
		amountStr = entry.Delta.Amount
	}
	if entry.Delta.Token != "" {
		asset = entry.Delta.Token
	}

	amount := cleanDecimal(amountStr)
	// Make amount positive (TransferInput.Amount is always positive, direction from Type)
	if strings.HasPrefix(amount, "-") {
		amount = amount[1:]
	}

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              transferType,
		Asset:             asset,
		Amount:            amount,
		Timestamp:         time.UnixMilli(entry.Time).UTC(),
	}
}

// cleanDecimal cleans a decimal string: trims trailing zeros and dots.
func cleanDecimal(s string) string {
	if s == "" {
		return "0"
	}
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// Compile-time interface check
var _ iface.ExchangeClient = (*Client)(nil)
