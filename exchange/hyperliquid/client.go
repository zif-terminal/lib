package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange"
	"github.com/zif-terminal/lib/models"
)

// Client implements exchange.ExchangeClient for Hyperliquid
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Hyperliquid client
func NewClient() *Client {
	return &Client{
		baseURL:    "https://api.hyperliquid.xyz",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "hyperliquid"
}

// FetchTrades fetches trades directly from Hyperliquid API
// Transforms exchange response directly to []*models.TradeInput
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, error) {
	// Check if ctx is cancelled
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse account ID to UUID
	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	// Extract address from account identifier
	address := account.AccountIdentifier
	if address == "" {
		return nil, fmt.Errorf("account identifier (address) is required")
	}

	// Build API request body
	// Based on Hyperliquid API: POST /info with {"type": "userFills", "user": address}
	requestBody := map[string]interface{}{
		"type": "userFills",
		"user": address,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make HTTP request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trades: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limit (HTTP 429)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &exchange.RateLimitError{
			Exchange:   "hyperliquid",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	// Check for other HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Parse response - API returns a direct array of fills, not wrapped in an object
	var apiFills []hyperliquidFill
	if err := json.NewDecoder(resp.Body).Decode(&apiFills); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Transform to TradeInput
	trades := make([]*models.TradeInput, 0)
	for _, apiFill := range apiFills {
		// Parse timestamp first
		tradeTimestamp := parseTimestamp(apiFill.Time)
		
		// Filter: only trades >= since
		if !since.IsZero() && tradeTimestamp.Before(since) {
			continue
		}

		tradeInput, err := transformFill(apiFill, accountUUID)
		if err != nil {
			// Skip fills that fail to transform
			continue
		}
		trades = append(trades, tradeInput)
	}

	// Sort by timestamp (oldest first) for incremental syncing
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	return trades, nil
}

// transformFill converts Hyperliquid fill format to TradeInput
func transformFill(apiFill hyperliquidFill, accountUUID uuid.UUID) (*models.TradeInput, error) {
	// Normalize side: Hyperliquid uses "B" for buy, "S" for sell, or "A" for close
	side := normalizeSide(apiFill.Side)

	// Parse timestamp (Hyperliquid returns Unix timestamp in milliseconds)
	timestamp := parseTimestamp(apiFill.Time)

	// Convert numeric fields to strings
	price := convertToString(apiFill.Px)
	quantity := convertToString(apiFill.Sz)
	fee := convertToString(apiFill.Fee)

	// Extract base and quote assets from coin (e.g., "BTC" from "BTC-USDC" or just "BTC")
	baseAsset, quoteAsset := parseAssetPair(apiFill.Coin)

	// Convert order ID to string
	orderID := convertToString(apiFill.Oid)

	return &models.TradeInput{
		TradeID:          apiFill.Hash, // Use transaction hash as trade ID
		OrderID:          orderID,      // Order ID (converted to string)
		BaseAsset:        baseAsset,
		QuoteAsset:       quoteAsset,
		Side:             side,
		Price:            price,
		Quantity:         quantity,
		Fee:              fee,
		Timestamp:        timestamp,
		ExchangeAccountID: accountUUID,
	}, nil
}

// normalizeSide converts Hyperliquid side format to "buy" or "sell"
// Hyperliquid uses: "B" (buy), "S" (sell), "A" (close/liquidation)
func normalizeSide(side string) string {
	side = strings.ToUpper(strings.TrimSpace(side))
	switch side {
	case "B", "BUY", "LONG":
		return "buy"
	case "S", "SELL", "SHORT":
		return "sell"
	case "A", "CLOSE", "LIQUIDATION":
		// Close/liquidation is typically a sell
		return "sell"
	default:
		// Default to buy if unknown
		return "buy"
	}
}

// parseTimestamp converts Hyperliquid timestamp to time.Time
// Hyperliquid returns Unix timestamp in milliseconds
func parseTimestamp(ts interface{}) time.Time {
	switch v := ts.(type) {
	case float64:
		// Unix timestamp in milliseconds
		return time.Unix(0, int64(v)*int64(time.Millisecond))
	case int64:
		return time.Unix(0, v*int64(time.Millisecond))
	case string:
		// Try parsing as Unix timestamp (milliseconds)
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(0, ms*int64(time.Millisecond))
		}
		// Try parsing as RFC3339
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseAssetPair extracts base and quote assets from coin string
// Hyperliquid format: "BTC-USDC" or "BTC"
func parseAssetPair(coin string) (baseAsset, quoteAsset string) {
	parts := strings.Split(coin, "-")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	// If no separator, assume USDC as quote (common for Hyperliquid)
	return parts[0], "USDC"
}

// convertToString converts numeric values to string for precision
func convertToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// Format with enough precision
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// parseRetryAfter parses Retry-After header (seconds)
func parseRetryAfter(retryAfter string) time.Duration {
	if retryAfter == "" {
		return 0
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
