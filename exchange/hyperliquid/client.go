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
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// Client implements iface.ExchangeClient for Hyperliquid
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
// Implements pagination to fetch all historical trades (API limits to 2000 per request)
// Uses userFillsByTime endpoint which returns trades in chronological order (oldest first)
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

	// Hyperliquid API has a limit of 2000 trades per request
	// We paginate forward using startTime to fetch all historical trades
	// userFillsByTime returns trades in chronological order (oldest first)
	const maxTradesPerRequest = 2000
	allTrades := make([]*models.TradeInput, 0)
	
	// Determine initial startTime for pagination
	// If since is zero, fetch all historical trades from the beginning
	// Otherwise, fetch trades starting from the 'since' timestamp
	var startTime int64
	if since.IsZero() {
		startTime = 0 // Fetch all historical trades
	} else {
		startTime = since.UnixMilli() // Fetch trades >= since
	}

	for {
		// Check if ctx is cancelled before each request
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Build API request body
		// Based on Hyperliquid API: POST /info with {"type": "userFillsByTime", "user": address, "startTime": startTime}
		requestBody := map[string]interface{}{
			"type":      "userFillsByTime",
			"user":      address,
			"startTime": startTime,
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

		// Check for rate limit (HTTP 429)
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			return nil, &iface.RateLimitError{
				Exchange:   "hyperliquid",
				Message:    "rate limit exceeded",
				RetryAfter: retryAfter,
			}
		}

		// Check for other HTTP errors
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		// Parse response - API returns a direct array of fills, not wrapped in an object
		var apiFills []hyperliquidFill
		if err := json.NewDecoder(resp.Body).Decode(&apiFills); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		// If no fills returned, we've reached the end
		if len(apiFills) == 0 {
			break
		}

		// Transform to TradeInput and collect
		batchTrades := make([]*models.TradeInput, 0, len(apiFills))
		var newestTimestamp *time.Time

		for _, apiFill := range apiFills {
			// Parse timestamp first
			tradeTimestamp := parseTimestamp(apiFill.Time)
			if tradeTimestamp.IsZero() {
				continue // Skip invalid timestamps
			}

			// Track newest timestamp for pagination (trades are returned oldest-first)
			if newestTimestamp == nil || tradeTimestamp.After(*newestTimestamp) {
				newestTimestamp = &tradeTimestamp
			}

			// Filter: only trades >= since (already handled by API startTime, but double-check for safety)
			if !since.IsZero() && tradeTimestamp.Before(since) {
				continue
			}

			tradeInput, err := transformFill(apiFill, accountUUID)
			if err != nil {
				// Return error instead of skipping - we're in dev phase and this should not happen
				// Missing required fields (e.g., tid) indicate a problem that needs investigation
				return nil, fmt.Errorf("failed to transform fill: %w | hash=%s | coin=%s | time=%v", err, apiFill.Hash, apiFill.Coin, apiFill.Time)
			}
			batchTrades = append(batchTrades, tradeInput)
		}

		// Add batch trades to all trades
		allTrades = append(allTrades, batchTrades...)

		// If we got fewer than maxTradesPerRequest, we've reached the end
		if len(apiFills) < maxTradesPerRequest {
			break
		}

		// If we didn't find any valid timestamps, break to avoid infinite loop
		if newestTimestamp == nil {
			break
		}

		// Set startTime to the newest timestamp + 1ms for next pagination request
		// This ensures we don't fetch the same trade again and continue forward
		startTime = newestTimestamp.UnixMilli() + 1
	}

	// Trades are already sorted chronologically (oldest first) from userFillsByTime
	// No need to sort again, but we verify for safety
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Timestamp.Before(allTrades[j].Timestamp)
	})

	return allTrades, nil
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

	// Convert fill ID (tid) to string for trade ID - tid is unique per fill
	// This ensures each fill has a unique trade_id, unlike Hash which can be shared across multiple fills
	// tid is REQUIRED - we cannot fall back to hash as it's not unique per fill
	if apiFill.Tid == nil {
		return nil, fmt.Errorf("missing required field 'tid' (fill ID) for fill with hash %s", apiFill.Hash)
	}
	
	tradeID := convertToString(apiFill.Tid)
	if tradeID == "" || tradeID == "<nil>" {
		return nil, fmt.Errorf("empty or invalid 'tid' (fill ID) for fill with hash %s, tid value: %v (type: %T)", apiFill.Hash, apiFill.Tid, apiFill.Tid)
	}

	return &models.TradeInput{
		TradeID:          tradeID,      // Use fill ID (tid) as trade ID - unique per fill
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
// Returns time in UTC to ensure consistent timezone handling
func parseTimestamp(ts interface{}) time.Time {
	var t time.Time
	switch v := ts.(type) {
	case float64:
		// Unix timestamp in milliseconds
		t = time.Unix(0, int64(v)*int64(time.Millisecond))
	case int64:
		t = time.Unix(0, v*int64(time.Millisecond))
	case string:
		// Try parsing as Unix timestamp (milliseconds)
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			t = time.Unix(0, ms*int64(time.Millisecond))
		} else if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			// Try parsing as RFC3339
			t = parsed
		} else {
			return time.Time{}
		}
	default:
		return time.Time{}
	}
	// Convert to UTC to ensure consistent timezone handling
	return t.UTC()
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
	if v == nil {
		return ""
	}
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

// FetchFundingPayments fetches funding payments directly from Hyperliquid API
// Transforms exchange response directly to []*models.FundingPaymentInput
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.FundingPaymentInput, error) {
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
	// Based on Hyperliquid API: POST /info with {"type": "userFunding", "user": address}
	requestBody := map[string]interface{}{
		"type": "userFunding",
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
		return nil, fmt.Errorf("failed to fetch funding payments: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limit (HTTP 429)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &iface.RateLimitError{
			Exchange:   "hyperliquid",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	// Check for other HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Parse response - API returns a direct array of funding payments, not wrapped in an object
	var apiPayments []hyperliquidFundingPayment
	if err := json.NewDecoder(resp.Body).Decode(&apiPayments); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Transform to FundingPaymentInput
	payments := make([]*models.FundingPaymentInput, 0)
	for _, apiPayment := range apiPayments {
		// Parse timestamp first
		paymentTimestamp := parseTimestamp(apiPayment.Time)

		// Filter: only payments >= since
		if !since.IsZero() && paymentTimestamp.Before(since) {
			continue
		}

		paymentInput, err := transformFundingPayment(apiPayment, accountUUID)
		if err != nil {
			// Return error instead of skipping - missing required fields indicate a problem
			return nil, fmt.Errorf("failed to transform funding payment: %w | hash=%s | coin=%s | time=%v", err, apiPayment.Hash, apiPayment.Delta.Coin, apiPayment.Time)
		}
		payments = append(payments, paymentInput)
	}

	// Sort by timestamp (oldest first) for incremental syncing
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Timestamp.Before(payments[j].Timestamp)
	})

	return payments, nil
}

// transformFundingPayment converts Hyperliquid funding payment format to FundingPaymentInput
func transformFundingPayment(apiPayment hyperliquidFundingPayment, accountUUID uuid.UUID) (*models.FundingPaymentInput, error) {
	// Parse timestamp (Hyperliquid returns Unix timestamp in milliseconds)
	timestamp := parseTimestamp(apiPayment.Time)
	if timestamp.IsZero() {
		return nil, fmt.Errorf("missing or invalid timestamp")
	}

	// Convert funding amount (USDC) to string (handles positive/negative)
	amount := convertToString(apiPayment.Delta.USDC)

	// Extract base and quote assets from coin (e.g., "SOL" -> base="SOL", quote="USDC")
	baseAsset, quoteAsset := parseAssetPair(apiPayment.Delta.Coin)
	if baseAsset == "" {
		return nil, fmt.Errorf("missing required field 'coin' (base asset)")
	}

	// Generate unique payment ID from timestamp + coin
	// Note: Hyperliquid's hash field is not unique (all payments have 0x0000...)
	// We use a composite key: timestamp_ms_coin to ensure uniqueness
	// Format: {timestamp_ms}_{coin}
	paymentID := fmt.Sprintf("%d_%s", timestamp.UnixMilli(), baseAsset)

	return &models.FundingPaymentInput{
		ExchangeAccountID: accountUUID,
		BaseAsset:         baseAsset,
		QuoteAsset:        quoteAsset,
		Amount:            amount,
		Timestamp:         timestamp,
		PaymentID:         paymentID,
	}, nil
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
