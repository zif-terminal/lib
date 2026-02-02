package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// Client implements iface.ExchangeClient for Drift
type Client struct {
	baseURL     string
	httpClient  *http.Client
	marketCache *marketCache
}

// NewClient creates a new Drift client
func NewClient() *Client {
	return &Client{
		baseURL:     "https://data.api.drift.trade",
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		marketCache: newMarketCache(1 * time.Hour), // Cache markets for 1 hour
	}
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "drift"
}

// FetchTrades fetches trades from Drift API
// Transforms exchange response directly to []*models.TradeInput
// Implements pagination to fetch all historical trades
// Drift API returns trades newest→oldest, so we reverse at the end
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

	// Extract account identifier (Drift subaccount public key)
	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	// Determine if we need to fetch historical data (older than 31 days)
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)

	// Determine the effective "since" date for historical fetching
	// If since is zero, we want ALL historical data - use Drift launch date (Nov 2021)
	var effectiveSince time.Time
	if since.IsZero() {
		effectiveSince = time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC)
	} else {
		effectiveSince = since
	}

	needsHistorical := effectiveSince.Before(thirtyOneDaysAgo)

	var allTrades []*models.TradeInput

	// Fetch historical months if needed
	if needsHistorical {
		historicalTrades, err := c.fetchHistoricalTrades(ctx, accountID, accountUUID, effectiveSince, thirtyOneDaysAgo)
		if err != nil {
			return nil, err
		}
		allTrades = append(allTrades, historicalTrades...)
	}

	// Fetch recent trades (last 31 days)
	recentTrades, err := c.fetchRecentTrades(ctx, accountID, accountUUID)
	if err != nil {
		return nil, err
	}
	allTrades = append(allTrades, recentTrades...)

	// Filter by since timestamp
	if !since.IsZero() {
		filtered := make([]*models.TradeInput, 0)
		for _, trade := range allTrades {
			if !trade.Timestamp.Before(since) {
				filtered = append(filtered, trade)
			}
		}
		allTrades = filtered
	}

	// Deduplicate by TradeID
	allTrades = deduplicateTrades(allTrades)

	// Sort by timestamp (oldest first)
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Timestamp.Before(allTrades[j].Timestamp)
	})

	return allTrades, nil
}

// fetchRecentTrades fetches trades from the last 31 days
func (c *Client) fetchRecentTrades(ctx context.Context, accountID string, accountUUID uuid.UUID) ([]*models.TradeInput, error) {
	var allTrades []*models.TradeInput
	var page string // Empty for first request, API returns nextPage token for pagination

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var url string
		if page == "" {
			url = fmt.Sprintf("%s/user/%s/trades", c.baseURL, accountID)
		} else {
			url = fmt.Sprintf("%s/user/%s/trades?page=%s", c.baseURL, accountID, page)
		}
		trades, nextPage, err := c.fetchTradesPage(ctx, url, accountUUID)
		if err != nil {
			return nil, err
		}

		allTrades = append(allTrades, trades...)

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return allTrades, nil
}

// fetchHistoricalTrades fetches trades from historical months
func (c *Client) fetchHistoricalTrades(ctx context.Context, accountID string, accountUUID uuid.UUID, since, until time.Time) ([]*models.TradeInput, error) {
	var allTrades []*models.TradeInput

	// Iterate through months from since to until
	current := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(until.Year(), until.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !current.After(end) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		monthTrades, err := c.fetchTradesForMonth(ctx, accountID, accountUUID, current.Year(), int(current.Month()))
		if err != nil {
			// Log but continue - some months may not have data
			current = current.AddDate(0, 1, 0)
			continue
		}

		allTrades = append(allTrades, monthTrades...)
		current = current.AddDate(0, 1, 0)
	}

	return allTrades, nil
}

// fetchTradesForMonth fetches trades for a specific year/month
func (c *Client) fetchTradesForMonth(ctx context.Context, accountID string, accountUUID uuid.UUID, year, month int) ([]*models.TradeInput, error) {
	var allTrades []*models.TradeInput
	var page string // Empty for first request

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var url string
		if page == "" {
			url = fmt.Sprintf("%s/user/%s/trades/%d/%d", c.baseURL, accountID, year, month)
		} else {
			url = fmt.Sprintf("%s/user/%s/trades/%d/%d?page=%s", c.baseURL, accountID, year, month, page)
		}
		trades, nextPage, err := c.fetchTradesPage(ctx, url, accountUUID)
		if err != nil {
			return nil, err
		}

		allTrades = append(allTrades, trades...)

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return allTrades, nil
}

// fetchTradesPage fetches a single page of trades
func (c *Client) fetchTradesPage(ctx context.Context, url string, accountUUID uuid.UUID) ([]*models.TradeInput, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch trades: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limit (HTTP 429)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, "", &iface.RateLimitError{
			Exchange:   "drift",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var response driftTradesResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, "", fmt.Errorf("API returned success=false")
	}

	// Transform records to TradeInput
	trades := make([]*models.TradeInput, 0, len(response.Records))
	for _, record := range response.Records {
		trade, err := c.transformTrade(ctx, record, accountUUID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to transform trade: %w", err)
		}
		trades = append(trades, trade)
	}

	// Get next page
	nextPage := ""
	if response.Meta.NextPage != nil {
		switch v := response.Meta.NextPage.(type) {
		case string:
			nextPage = v
		case float64:
			nextPage = strconv.Itoa(int(v))
		}
	}

	return trades, nextPage, nil
}

// transformTrade converts a Drift trade record to TradeInput
func (c *Client) transformTrade(ctx context.Context, record driftTradeRecord, accountUUID uuid.UUID) (*models.TradeInput, error) {
	// Determine if user is taker or maker
	isTaker := record.User == record.Taker

	// Get side based on whether user is taker or maker
	var direction string
	if isTaker {
		direction = record.TakerOrderDirection
	} else {
		direction = record.MakerOrderDirection
	}
	side := normalizeSide(direction)

	// Get order ID based on role
	var orderID string
	if isTaker {
		orderID = record.TakerOrderID
	} else {
		orderID = record.MakerOrderID
	}

	// Get fee based on role (API returns already-formatted decimals)
	var fee string
	if isTaker {
		fee = record.TakerFee
	} else {
		fee = record.MakerFee
	}
	// Clean up the fee value (remove trailing zeros)
	fee = cleanDecimalString(fee)

	// Parse symbol to get base/quote assets
	// Drift format: "SOL-PERP" or "BTC-PERP"
	baseAsset, quoteAsset := parseSymbol(record.Symbol)

	// API returns already-formatted decimal values
	baseAmount := cleanDecimalString(record.BaseAssetAmountFilled)
	quoteAmount := cleanDecimalString(record.QuoteAssetAmountFilled)

	// Calculate price = quoteAssetAmountFilled / baseAssetAmountFilled
	price := calculatePrice(quoteAmount, baseAmount)

	// Convert timestamp (Drift uses seconds, not milliseconds)
	timestamp := time.Unix(record.Ts, 0).UTC()

	return &models.TradeInput{
		TradeID:           record.FillRecordID,
		OrderID:           orderID,
		BaseAsset:         baseAsset,
		QuoteAsset:        quoteAsset,
		Side:              side,
		Price:             price,
		Quantity:          baseAmount,
		Fee:               fee,
		Timestamp:         timestamp,
		ExchangeAccountID: accountUUID,
	}, nil
}

// FetchFundingPayments fetches funding payments from Drift API
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

	// Extract account identifier
	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	// Determine if we need to fetch historical data
	thirtyOneDaysAgo := time.Now().AddDate(0, 0, -31)

	// Determine the effective "since" date for historical fetching
	// If since is zero, we want ALL historical data - use Drift launch date (Nov 2021)
	var effectiveSince time.Time
	if since.IsZero() {
		effectiveSince = time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC)
	} else {
		effectiveSince = since
	}

	needsHistorical := effectiveSince.Before(thirtyOneDaysAgo)

	var allPayments []*models.FundingPaymentInput

	// Fetch historical months if needed
	if needsHistorical {
		historicalPayments, err := c.fetchHistoricalFunding(ctx, accountID, accountUUID, effectiveSince, thirtyOneDaysAgo)
		if err != nil {
			return nil, err
		}
		allPayments = append(allPayments, historicalPayments...)
	}

	// Fetch recent funding payments (last 31 days)
	recentPayments, err := c.fetchRecentFunding(ctx, accountID, accountUUID)
	if err != nil {
		return nil, err
	}
	allPayments = append(allPayments, recentPayments...)

	// Filter by since timestamp
	if !since.IsZero() {
		filtered := make([]*models.FundingPaymentInput, 0)
		for _, payment := range allPayments {
			if !payment.Timestamp.Before(since) {
				filtered = append(filtered, payment)
			}
		}
		allPayments = filtered
	}

	// Deduplicate by PaymentID
	allPayments = deduplicateFunding(allPayments)

	// Sort by timestamp (oldest first)
	sort.Slice(allPayments, func(i, j int) bool {
		return allPayments[i].Timestamp.Before(allPayments[j].Timestamp)
	})

	return allPayments, nil
}

// fetchRecentFunding fetches funding payments from the last 31 days
func (c *Client) fetchRecentFunding(ctx context.Context, accountID string, accountUUID uuid.UUID) ([]*models.FundingPaymentInput, error) {
	var allPayments []*models.FundingPaymentInput
	var page string // Empty for first request

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var url string
		if page == "" {
			url = fmt.Sprintf("%s/user/%s/fundingPayments", c.baseURL, accountID)
		} else {
			url = fmt.Sprintf("%s/user/%s/fundingPayments?page=%s", c.baseURL, accountID, page)
		}
		payments, nextPage, err := c.fetchFundingPage(ctx, url, accountUUID)
		if err != nil {
			return nil, err
		}

		allPayments = append(allPayments, payments...)

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return allPayments, nil
}

// fetchHistoricalFunding fetches funding payments from historical months
func (c *Client) fetchHistoricalFunding(ctx context.Context, accountID string, accountUUID uuid.UUID, since, until time.Time) ([]*models.FundingPaymentInput, error) {
	var allPayments []*models.FundingPaymentInput

	current := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(until.Year(), until.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !current.After(end) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		monthPayments, err := c.fetchFundingForMonth(ctx, accountID, accountUUID, current.Year(), int(current.Month()))
		if err != nil {
			current = current.AddDate(0, 1, 0)
			continue
		}

		allPayments = append(allPayments, monthPayments...)
		current = current.AddDate(0, 1, 0)
	}

	return allPayments, nil
}

// fetchFundingForMonth fetches funding payments for a specific year/month
func (c *Client) fetchFundingForMonth(ctx context.Context, accountID string, accountUUID uuid.UUID, year, month int) ([]*models.FundingPaymentInput, error) {
	var allPayments []*models.FundingPaymentInput
	var page string // Empty for first request

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		var url string
		if page == "" {
			url = fmt.Sprintf("%s/user/%s/fundingPayments/%d/%d", c.baseURL, accountID, year, month)
		} else {
			url = fmt.Sprintf("%s/user/%s/fundingPayments/%d/%d?page=%s", c.baseURL, accountID, year, month, page)
		}
		payments, nextPage, err := c.fetchFundingPage(ctx, url, accountUUID)
		if err != nil {
			return nil, err
		}

		allPayments = append(allPayments, payments...)

		if nextPage == "" {
			break
		}
		page = nextPage
	}

	return allPayments, nil
}

// fetchFundingPage fetches a single page of funding payments
func (c *Client) fetchFundingPage(ctx context.Context, url string, accountUUID uuid.UUID) ([]*models.FundingPaymentInput, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch funding payments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, "", &iface.RateLimitError{
			Exchange:   "drift",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var response driftFundingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, "", fmt.Errorf("API returned success=false")
	}

	// Transform records to FundingPaymentInput
	payments := make([]*models.FundingPaymentInput, 0, len(response.Records))
	for _, record := range response.Records {
		payment, err := c.transformFundingPayment(ctx, record, accountUUID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to transform funding payment: %w", err)
		}
		payments = append(payments, payment)
	}

	// Get next page
	nextPage := ""
	if response.Meta.NextPage != nil {
		switch v := response.Meta.NextPage.(type) {
		case string:
			nextPage = v
		case float64:
			nextPage = strconv.Itoa(int(v))
		}
	}

	return payments, nextPage, nil
}

// transformFundingPayment converts a Drift funding payment record to FundingPaymentInput
func (c *Client) transformFundingPayment(ctx context.Context, record driftFundingPayment, accountUUID uuid.UUID) (*models.FundingPaymentInput, error) {
	// Get market info to determine base asset
	marketInfo, err := c.getMarketInfo(ctx, record.MarketIndex, "perp")
	if err != nil {
		return nil, fmt.Errorf("failed to get market info for index %d: %w", record.MarketIndex, err)
	}

	// API returns already-formatted decimal values
	amount := cleanDecimalString(record.FundingPayment)

	// Generate unique payment ID: {ts}_{marketIndex}
	paymentID := fmt.Sprintf("%d_%d", record.Ts, record.MarketIndex)

	// Convert timestamp
	timestamp := time.Unix(record.Ts, 0).UTC()

	return &models.FundingPaymentInput{
		ExchangeAccountID: accountUUID,
		BaseAsset:         marketInfo.BaseAsset,
		QuoteAsset:        marketInfo.QuoteAsset,
		Amount:            amount,
		Timestamp:         timestamp,
		PaymentID:         paymentID,
	}, nil
}

// Helper functions

// normalizeSide converts Drift direction to "buy" or "sell"
func normalizeSide(direction string) string {
	direction = strings.ToLower(strings.TrimSpace(direction))
	switch direction {
	case "long":
		return "buy"
	case "short":
		return "sell"
	default:
		return "buy" // Default to buy if unknown
	}
}

// parseSymbol extracts base and quote assets from Drift symbol
// Drift format: "SOL-PERP" -> base="SOL", quote="USDC"
func parseSymbol(symbol string) (baseAsset, quoteAsset string) {
	parts := strings.Split(symbol, "-")
	if len(parts) >= 1 {
		baseAsset = parts[0]
	}
	quoteAsset = "USDC" // Drift uses USDC for all perp markets
	return
}

// cleanDecimalString removes trailing zeros from a decimal string
func cleanDecimalString(s string) string {
	if s == "" {
		return "0"
	}
	// Trim trailing zeros after decimal point
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// convertFromBasePrecision converts raw base amount to decimal string
func convertFromBasePrecision(raw string) string {
	if raw == "" {
		return "0"
	}
	return divideByPrecision(raw, BasePrecision)
}

// convertFromQuotePrecision converts raw quote amount to decimal string
func convertFromQuotePrecision(raw string) string {
	if raw == "" {
		return "0"
	}
	return divideByPrecision(raw, QuotePrecision)
}

// divideByPrecision divides a raw integer string by precision
func divideByPrecision(raw string, precision int64) string {
	// Parse the raw value as big.Int to handle large numbers
	rawInt := new(big.Int)
	rawInt, ok := rawInt.SetString(raw, 10)
	if !ok {
		return "0"
	}

	// Handle negative values
	negative := rawInt.Sign() < 0
	if negative {
		rawInt = rawInt.Abs(rawInt)
	}

	// Divide by precision using big.Float for decimal handling
	rawFloat := new(big.Float).SetInt(rawInt)
	precFloat := new(big.Float).SetInt64(precision)
	result := new(big.Float).Quo(rawFloat, precFloat)

	// Format with appropriate precision
	str := result.Text('f', 18) // Use 18 decimal places max

	// Trim trailing zeros
	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	if negative {
		str = "-" + str
	}

	return str
}

// calculatePrice calculates price from quote and base amounts
func calculatePrice(quoteAmount, baseAmount string) string {
	if baseAmount == "" || baseAmount == "0" {
		return "0"
	}

	quoteFloat, _, err := big.ParseFloat(quoteAmount, 10, 256, big.ToNearestEven)
	if err != nil {
		return "0"
	}

	baseFloat, _, err := big.ParseFloat(baseAmount, 10, 256, big.ToNearestEven)
	if err != nil {
		return "0"
	}

	if baseFloat.Sign() == 0 {
		return "0"
	}

	result := new(big.Float).Quo(quoteFloat, baseFloat)
	str := result.Text('f', 18)

	// Trim trailing zeros
	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	return str
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

// deduplicateTrades removes duplicate trades by TradeID
func deduplicateTrades(trades []*models.TradeInput) []*models.TradeInput {
	seen := make(map[string]bool)
	result := make([]*models.TradeInput, 0, len(trades))

	for _, trade := range trades {
		if !seen[trade.TradeID] {
			seen[trade.TradeID] = true
			result = append(result, trade)
		}
	}

	return result
}

// deduplicateFunding removes duplicate funding payments by PaymentID
func deduplicateFunding(payments []*models.FundingPaymentInput) []*models.FundingPaymentInput {
	seen := make(map[string]bool)
	result := make([]*models.FundingPaymentInput, 0, len(payments))

	for _, payment := range payments {
		if !seen[payment.PaymentID] {
			seen[payment.PaymentID] = true
			result = append(result, payment)
		}
	}

	return result
}
