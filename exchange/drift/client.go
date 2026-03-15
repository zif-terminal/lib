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

// Retry configuration for rate limiting
const (
	maxRetries       = 5
	initialBackoff   = 2 * time.Second
	maxBackoff       = 60 * time.Second
	backoffMultipler = 2.0
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
		marketCache: newMarketCache(1 * time.Hour),
	}
}

// doRequestWithRetry executes an HTTP request with exponential backoff for rate limits
func (c *Client) doRequestWithRetry(ctx context.Context, url string) (*http.Response, error) {
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		// Check for rate limiting (429 or 403)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()

			if attempt == maxRetries {
				return nil, &iface.RateLimitError{
					Exchange:   "drift",
					Message:    fmt.Sprintf("rate limit exceeded after %d retries", maxRetries),
					RetryAfter: backoff,
				}
			}

			if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > 0 {
				backoff = retryAfter
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			backoff = time.Duration(float64(backoff) * backoffMultipler)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "drift"
}

// FetchTrades fetches trades and swaps from Drift API
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	// Fetch trades using generic pagination
	trades, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"trades",
		since,
		c.createTradePageFetcher(accountUUID),
		func(t *models.TradeInput) time.Time { return t.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	// Fetch swaps using generic pagination
	swaps, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"swaps",
		since,
		c.createSwapPageFetcher(accountUUID),
		func(t *models.TradeInput) time.Time { return t.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	// Combine trades and swaps
	allTrades := append(trades, swaps...)

	// Sort by timestamp (oldest first)
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Timestamp.Before(allTrades[j].Timestamp)
	})

	return allTrades, nil
}

// FetchFundingPayments fetches funding payments from Drift API
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

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	payments, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"fundingPayments",
		since,
		c.createFundingPageFetcher(accountUUID),
		func(p *models.TransferInput) time.Time { return p.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp (oldest first)
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Timestamp.Before(payments[j].Timestamp)
	})

	return payments, nil
}

// FetchDeposits fetches deposits and withdrawals from Drift API
func (c *Client) FetchDeposits(
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

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	deposits, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"deposits",
		since,
		c.createDepositPageFetcher(accountUUID),
		func(d *models.TransferInput) time.Time { return d.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp (oldest first)
	sort.Slice(deposits, func(i, j int) bool {
		return deposits[i].Timestamp.Before(deposits[j].Timestamp)
	})

	return deposits, nil
}

// Page fetcher factories - these create page fetchers for each data type

func (c *Client) createTradePageFetcher(accountUUID uuid.UUID) pageFetcher[*models.TradeInput] {
	return func(ctx context.Context, url string) (pageResult[*models.TradeInput], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("failed to fetch trades: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftTradesResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("API returned success=false")
		}

		trades := make([]*models.TradeInput, 0, len(response.Records))
		for _, record := range response.Records {
			trade, err := c.transformTrade(ctx, record, accountUUID)
			if err != nil {
				return pageResult[*models.TradeInput]{}, fmt.Errorf("failed to transform trade: %w", err)
			}
			trades = append(trades, trade)
		}

		return pageResult[*models.TradeInput]{
			items:    trades,
			nextPage: extractNextPage(response.Meta),
		}, nil
	}
}

func (c *Client) createSwapPageFetcher(accountUUID uuid.UUID) pageFetcher[*models.TradeInput] {
	return func(ctx context.Context, url string) (pageResult[*models.TradeInput], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("failed to fetch swaps: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftSwapsResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[*models.TradeInput]{}, fmt.Errorf("API returned success=false")
		}

		swaps := make([]*models.TradeInput, 0, len(response.Records))
		for _, record := range response.Records {
			swap := c.transformSwap(record, accountUUID)
			swaps = append(swaps, swap)
		}

		return pageResult[*models.TradeInput]{
			items:    swaps,
			nextPage: extractNextPage(response.Meta),
		}, nil
	}
}

func (c *Client) createFundingPageFetcher(accountUUID uuid.UUID) pageFetcher[*models.TransferInput] {
	return func(ctx context.Context, url string) (pageResult[*models.TransferInput], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to fetch funding payments: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftFundingResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("API returned success=false")
		}

		payments := make([]*models.TransferInput, 0, len(response.Records))
		for _, record := range response.Records {
			payment, err := c.transformFundingPayment(ctx, record, accountUUID)
			if err != nil {
				return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to transform funding payment: %w", err)
			}
			payments = append(payments, payment)
		}

		return pageResult[*models.TransferInput]{
			items:    payments,
			nextPage: extractNextPage(response.Meta),
		}, nil
	}
}

func (c *Client) createDepositPageFetcher(accountUUID uuid.UUID) pageFetcher[*models.TransferInput] {
	return func(ctx context.Context, url string) (pageResult[*models.TransferInput], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to fetch deposits: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftDepositsResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("API returned success=false")
		}

		deposits := make([]*models.TransferInput, 0, len(response.Records))
		for _, record := range response.Records {
			deposit, err := c.transformDeposit(ctx, record, accountUUID)
			if err != nil {
				return pageResult[*models.TransferInput]{}, fmt.Errorf("failed to transform deposit: %w", err)
			}
			deposits = append(deposits, deposit)
		}

		return pageResult[*models.TransferInput]{
			items:    deposits,
			nextPage: extractNextPage(response.Meta),
		}, nil
	}
}

// SettlePnlRecord represents a PnL settlement event from Drift (internal type)
type SettlePnlRecord struct {
	Timestamp   time.Time
	Pnl         string // Settled PnL amount (signed)
	MarketIndex int
}

// FetchSettlements implements iface.ExchangeClient.
// On Drift, PnL is settled by keeper bots separately from trade execution.
// This returns the actual settlePnl records from the Drift API.
func (c *Client) FetchSettlements(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.Settlement, error) {
	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	records, err := c.FetchSettlePnl(ctx, account, since)
	if err != nil {
		return nil, err
	}

	settlements := make([]*models.Settlement, 0, len(records))
	for _, r := range records {
		// Resolve market index to symbol
		marketInfo, err := c.getMarketInfo(ctx, r.MarketIndex, "perp")
		market := fmt.Sprintf("%d", r.MarketIndex)
		if err == nil {
			market = marketInfo.Symbol
		}

		settlements = append(settlements, &models.Settlement{
			ExchangeAccountID: accountUUID,
			Asset:             "USDC",
			Amount:            r.Pnl,
			Market:            market,
			Timestamp:         r.Timestamp,
			SettlementID:      fmt.Sprintf("settle_%d_%s", r.MarketIndex, r.Timestamp.Format("20060102150405")),
		})
	}

	return settlements, nil
}

// FetchSettlePnl fetches raw PnL settlement records from the Drift API (Drift-specific)
func (c *Client) FetchSettlePnl(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]SettlePnlRecord, error) {
	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier is required")
	}

	records, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"settlePnl",
		since,
		c.createSettlePnlPageFetcher(),
		func(r SettlePnlRecord) time.Time { return r.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})

	return records, nil
}

func (c *Client) createSettlePnlPageFetcher() pageFetcher[SettlePnlRecord] {
	return func(ctx context.Context, url string) (pageResult[SettlePnlRecord], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[SettlePnlRecord]{}, fmt.Errorf("failed to fetch settlePnl: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[SettlePnlRecord]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftSettlePnlResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[SettlePnlRecord]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[SettlePnlRecord]{}, fmt.Errorf("API returned success=false")
		}

		records := make([]SettlePnlRecord, 0, len(response.Records))
		for _, r := range response.Records {
			records = append(records, SettlePnlRecord{
				Timestamp:   time.Unix(r.Ts, 0).UTC(),
				Pnl:         cleanDecimalString(r.Pnl),
				MarketIndex: r.MarketIndex,
			})
		}

		return pageResult[SettlePnlRecord]{
			items:    records,
			nextPage: extractNextPage(response.Meta),
		}, nil
	}
}

// extractNextPage extracts the next page token from API response metadata
func extractNextPage(meta driftMeta) string {
	if meta.NextPage == nil {
		return ""
	}
	switch v := meta.NextPage.(type) {
	case string:
		return v
	case float64:
		return strconv.Itoa(int(v))
	}
	return ""
}

// Transform functions - convert API records to model types

func (c *Client) transformTrade(ctx context.Context, record driftTradeRecord, accountUUID uuid.UUID) (*models.TradeInput, error) {
	isTaker := record.User == record.Taker

	var direction string
	if isTaker {
		direction = record.TakerOrderDirection
	} else {
		direction = record.MakerOrderDirection
	}
	side := normalizeSide(direction)

	var orderID string
	if isTaker {
		orderID = record.TakerOrderID
	} else {
		orderID = record.MakerOrderID
	}

	var fee string
	if isTaker {
		fee = record.TakerFee
	} else {
		fee = record.MakerFee
	}
	fee = cleanDecimalString(fee)

	baseAsset, quoteAsset := parseSymbol(record.Symbol)
	baseAmount := cleanDecimalString(record.BaseAssetAmountFilled)
	quoteAmount := cleanDecimalString(record.QuoteAssetAmountFilled)
	price := calculatePrice(quoteAmount, baseAmount)
	timestamp := time.Unix(record.Ts, 0).UTC()

	marketType := strings.ToLower(record.MarketType)
	if marketType == "" {
		marketType = "perp"
	}

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
		MarketType:        marketType,
	}, nil
}

func (c *Client) transformSwap(record driftSwapRecord, accountUUID uuid.UUID) *models.TradeInput {
	amountIn := cleanDecimalString(record.AmountIn)
	amountOut := cleanDecimalString(record.AmountOut)
	fee := cleanDecimalString(record.Fee)

	var baseAsset, quoteAsset, side, quantity, price string

	if record.OutSymbol == "USDC" {
		baseAsset = record.InSymbol
		quoteAsset = "USDC"
		side = "sell"
		quantity = amountIn
		price = calculatePrice(amountOut, amountIn)
	} else if record.InSymbol == "USDC" {
		baseAsset = record.OutSymbol
		quoteAsset = "USDC"
		side = "buy"
		quantity = amountOut
		price = calculatePrice(amountIn, amountOut)
	} else {
		baseAsset = record.OutSymbol
		quoteAsset = record.InSymbol
		side = "buy"
		quantity = amountOut
		price = calculatePrice(amountIn, amountOut)
	}

	timestamp := time.Unix(record.Ts, 0).UTC()
	tradeID := fmt.Sprintf("swap_%s_%d", record.TxSig, record.TxSigIndex)

	return &models.TradeInput{
		TradeID:           tradeID,
		OrderID:           record.TxSig,
		BaseAsset:         baseAsset,
		QuoteAsset:        quoteAsset,
		Side:              side,
		Price:             price,
		Quantity:          quantity,
		Fee:               fee,
		Timestamp:         timestamp,
		ExchangeAccountID: accountUUID,
		MarketType:        "swap",
	}
}

func (c *Client) transformFundingPayment(ctx context.Context, record driftFundingPayment, accountUUID uuid.UUID) (*models.TransferInput, error) {
	marketInfo, err := c.getMarketInfo(ctx, record.MarketIndex, "perp")
	if err != nil {
		return nil, fmt.Errorf("failed to get market info for index %d: %w", record.MarketIndex, err)
	}

	amount := cleanDecimalString(record.FundingPayment)
	paymentID := fmt.Sprintf("%s_%d", record.TxSig, record.TxSigIndex)
	timestamp := time.Unix(record.Ts, 0).UTC()

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeFunding,
		Asset:             marketInfo.QuoteAsset,
		Amount:            amount,
		Timestamp:         timestamp,
		Metadata: map[string]string{
			"market":     marketInfo.BaseAsset,
			"payment_id": paymentID,
		},
	}, nil
}

func (c *Client) transformDeposit(ctx context.Context, record driftDepositRecord, accountUUID uuid.UUID) (*models.TransferInput, error) {
	marketInfo, err := c.getMarketInfo(ctx, record.MarketIndex, "spot")
	if err != nil {
		return nil, fmt.Errorf("failed to get market info for index %d: %w", record.MarketIndex, err)
	}

	amount := cleanDecimalString(record.Amount)
	costBasis := cleanDecimalString(record.OraclePrice)
	if costBasis == "" {
		costBasis = "0"
	}

	timestamp := time.Unix(record.Ts, 0).UTC()

	direction := strings.ToLower(record.Direction)
	transferType := models.TypeDeposit
	if direction == "withdraw" {
		transferType = models.TypeWithdraw
	}

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Asset:             marketInfo.BaseAsset,
		Type:              transferType,
		Amount:            amount,
		CostBasis:         costBasis,
		Timestamp:         timestamp,
	}, nil
}

// Helper functions

func normalizeSide(direction string) string {
	direction = strings.ToLower(strings.TrimSpace(direction))
	switch direction {
	case "long":
		return "buy"
	case "short":
		return "sell"
	default:
		return "buy"
	}
}

func parseSymbol(symbol string) (baseAsset, quoteAsset string) {
	parts := strings.Split(symbol, "-")
	if len(parts) >= 1 {
		baseAsset = parts[0]
	}
	quoteAsset = "USDC"
	return
}

func cleanDecimalString(s string) string {
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

func convertFromBasePrecision(raw string) string {
	if raw == "" {
		return "0"
	}
	return divideByPrecision(raw, BasePrecision)
}

func convertFromQuotePrecision(raw string) string {
	if raw == "" {
		return "0"
	}
	return divideByPrecision(raw, QuotePrecision)
}

func divideByPrecision(raw string, precision int64) string {
	rawInt := new(big.Int)
	rawInt, ok := rawInt.SetString(raw, 10)
	if !ok {
		return "0"
	}

	negative := rawInt.Sign() < 0
	if negative {
		rawInt = rawInt.Abs(rawInt)
	}

	rawFloat := new(big.Float).SetInt(rawInt)
	precFloat := new(big.Float).SetInt64(precision)
	result := new(big.Float).Quo(rawFloat, precFloat)

	str := result.Text('f', 18)

	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	if negative {
		str = "-" + str
	}

	return str
}

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

	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	return str
}

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
