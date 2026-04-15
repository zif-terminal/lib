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
	backoffMultiplier = 2.0
)

// oracleDenomination is the denomination used by Drift's oracle prices.
// Drift oracles report prices in USDC. Other exchanges may use different denominations.
const oracleDenomination = "USDC"

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

		if err := globalLimiter.Wait(ctx); err != nil {
			return nil, err
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

			backoff = time.Duration(float64(backoff) * backoffMultiplier)
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

// tradeWithPrices pairs a trade with its oracle price records
type tradeWithPrices struct {
	trade  *models.TradeInput
	prices []*models.PriceRecord
}

// FetchTrades fetches trades and swaps from Drift API
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

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	// Fetch trades using generic pagination
	tradeResults, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"trades",
		since,
		c.createTradePageFetcher(accountUUID),
		func(t tradeWithPrices) time.Time { return t.trade.Timestamp },
	)
	if err != nil {
		return nil, nil, err
	}

	// Fetch swaps using generic pagination
	swapResults, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"swaps",
		since,
		c.createSwapPageFetcher(accountUUID),
		func(t tradeWithPrices) time.Time { return t.trade.Timestamp },
	)
	if err != nil {
		return nil, nil, err
	}

	// Combine trades and swaps
	allResults := append(tradeResults, swapResults...)

	// Sort by timestamp (oldest first)
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].trade.Timestamp.Before(allResults[j].trade.Timestamp)
	})

	// Separate into trades and prices
	allTrades := make([]*models.TradeInput, 0, len(allResults))
	var allPrices []*models.PriceRecord
	for _, r := range allResults {
		allTrades = append(allTrades, r.trade)
		allPrices = append(allPrices, r.prices...)
	}

	return allTrades, allPrices, nil
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

// depositWithPrice pairs a transfer with its oracle price record
type depositWithPrice struct {
	transfer *models.TransferInput
	price    *models.PriceRecord
}

// FetchDeposits fetches deposits and withdrawals from Drift API
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

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	results, err := fetchWithHistory(
		ctx,
		c.baseURL,
		accountID,
		"deposits",
		since,
		c.createDepositPageFetcher(accountUUID),
		func(d depositWithPrice) time.Time { return d.transfer.Timestamp },
	)
	if err != nil {
		return nil, nil, err
	}

	// Sort by timestamp (oldest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].transfer.Timestamp.Before(results[j].transfer.Timestamp)
	})

	// Separate into transfers and prices
	deposits := make([]*models.TransferInput, 0, len(results))
	var prices []*models.PriceRecord
	for _, r := range results {
		deposits = append(deposits, r.transfer)
		if r.price != nil {
			prices = append(prices, r.price)
		}
	}

	return deposits, prices, nil
}

// Page fetcher factories - these create page fetchers for each data type

func (c *Client) createTradePageFetcher(accountUUID uuid.UUID) pageFetcher[tradeWithPrices] {
	return func(ctx context.Context, url string) (pageResult[tradeWithPrices], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("failed to fetch trades: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftTradesResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("API returned success=false")
		}

		// Pre-warm market cache before processing the batch
		_ = c.ensureMarketCache(ctx)

		results := make([]tradeWithPrices, 0, len(response.Records))
		for _, record := range response.Records {
			trade, price, err := c.transformTrade(ctx, record, accountUUID)
			if err != nil {
				return pageResult[tradeWithPrices]{}, fmt.Errorf("drift/trades: failed to transform trade %s (market_index=%d, market_type=%s, ts=%d): %w",
					record.FillRecordID, record.MarketIndex, record.MarketType, record.Ts, err)
			}
			var prices []*models.PriceRecord
			if price != nil {
				prices = []*models.PriceRecord{price}
			}
			results = append(results, tradeWithPrices{trade: trade, prices: prices})
		}

		nextPage, err := extractNextPage(response.Meta)
		if err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("drift/trades: %w", err)
		}
		return pageResult[tradeWithPrices]{
			items:    results,
			nextPage: nextPage,
		}, nil
	}
}

func (c *Client) createSwapPageFetcher(accountUUID uuid.UUID) pageFetcher[tradeWithPrices] {
	return func(ctx context.Context, url string) (pageResult[tradeWithPrices], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("failed to fetch swaps: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftSwapsResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("API returned success=false")
		}

		// Pre-warm market cache before processing the batch
		_ = c.ensureMarketCache(ctx)

		results := make([]tradeWithPrices, 0, len(response.Records))
		for _, record := range response.Records {
			swap, prices, err := c.transformSwap(ctx, record, accountUUID)
			if err != nil {
				return pageResult[tradeWithPrices]{}, fmt.Errorf("drift/swaps: failed to transform swap %s_%d (out_market=%d, in_market=%d, ts=%d): %w",
					record.TxSig, record.TxSigIndex, record.OutMarketIndex, record.InMarketIndex, record.Ts, err)
			}
			results = append(results, tradeWithPrices{trade: swap, prices: prices})
		}

		nextPage, err := extractNextPage(response.Meta)
		if err != nil {
			return pageResult[tradeWithPrices]{}, fmt.Errorf("drift/swaps: %w", err)
		}
		return pageResult[tradeWithPrices]{
			items:    results,
			nextPage: nextPage,
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

		// Pre-warm market cache before processing the batch
		_ = c.ensureMarketCache(ctx)

		payments := make([]*models.TransferInput, 0, len(response.Records))
		for _, record := range response.Records {
			payment, err := c.transformFundingPayment(ctx, record, accountUUID)
			if err != nil {
				return pageResult[*models.TransferInput]{}, fmt.Errorf("drift/funding: failed to transform payment (market_index=%d, ts=%d): %w",
					record.MarketIndex, record.Ts, err)
			}
			payments = append(payments, payment)
		}

		nextPage, err := extractNextPage(response.Meta)
		if err != nil {
			return pageResult[*models.TransferInput]{}, fmt.Errorf("drift/funding: %w", err)
		}
		return pageResult[*models.TransferInput]{
			items:    payments,
			nextPage: nextPage,
		}, nil
	}
}

func (c *Client) createDepositPageFetcher(accountUUID uuid.UUID) pageFetcher[depositWithPrice] {
	return func(ctx context.Context, url string) (pageResult[depositWithPrice], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[depositWithPrice]{}, fmt.Errorf("failed to fetch deposits: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[depositWithPrice]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftDepositsResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[depositWithPrice]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[depositWithPrice]{}, fmt.Errorf("API returned success=false")
		}

		// Pre-warm market cache before processing the batch
		_ = c.ensureMarketCache(ctx)

		results := make([]depositWithPrice, 0, len(response.Records))
		for _, record := range response.Records {
			transfer, price, err := c.transformDeposit(ctx, record, accountUUID)
			if err != nil {
				return pageResult[depositWithPrice]{}, fmt.Errorf("drift/deposits: failed to transform deposit %s (market_index=%d, ts=%d): %w",
					record.DepositRecordID, record.MarketIndex, record.Ts, err)
			}
			results = append(results, depositWithPrice{transfer: transfer, price: price})
		}

		nextPage, err := extractNextPage(response.Meta)
		if err != nil {
			return pageResult[depositWithPrice]{}, fmt.Errorf("drift/deposits: %w", err)
		}
		return pageResult[depositWithPrice]{
			items:    results,
			nextPage: nextPage,
		}, nil
	}
}

// settlePnlRecord represents a PnL settlement event from Drift (internal type)
type settlePnlRecord struct {
	Timestamp   time.Time
	Pnl         string // Settled PnL amount (signed)
	MarketIndex int
	TxSig       string
	TxSigIndex  int
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

	records, err := c.fetchSettlePnl(ctx, account, since)
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

		sid := settlementID(r)
		settlements = append(settlements, &models.Settlement{
			ExchangeAccountID: accountUUID,
			Asset:             "USDC",
			Amount:            r.Pnl,
			Market:            market,
			Timestamp:         r.Timestamp,
			SettlementID:      sid,
			ExternalID:        sid,
		})
	}

	return settlements, nil
}

// fetchSettlePnl fetches raw PnL settlement records from the Drift API
func (c *Client) fetchSettlePnl(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]settlePnlRecord, error) {
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
		func(r settlePnlRecord) time.Time { return r.Timestamp },
	)
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})

	return records, nil
}

func (c *Client) createSettlePnlPageFetcher() pageFetcher[settlePnlRecord] {
	return func(ctx context.Context, url string) (pageResult[settlePnlRecord], error) {
		resp, err := c.doRequestWithRetry(ctx, url)
		if err != nil {
			return pageResult[settlePnlRecord]{}, fmt.Errorf("failed to fetch settlePnl: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return pageResult[settlePnlRecord]{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		var response driftSettlePnlResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return pageResult[settlePnlRecord]{}, fmt.Errorf("failed to decode response: %w", err)
		}

		if !response.Success {
			return pageResult[settlePnlRecord]{}, fmt.Errorf("API returned success=false")
		}

		records := make([]settlePnlRecord, 0, len(response.Records))
		for _, r := range response.Records {
			records = append(records, settlePnlRecord{
				Timestamp:   time.Unix(r.Ts, 0).UTC(),
				Pnl:         cleanDecimalString(r.Pnl),
				MarketIndex: r.MarketIndex,
				TxSig:       r.TxSig,
				TxSigIndex:  r.TxSigIndex,
			})
		}

		nextPage, err := extractNextPage(response.Meta)
		if err != nil {
			return pageResult[settlePnlRecord]{}, fmt.Errorf("drift/settlePnl: %w", err)
		}
		return pageResult[settlePnlRecord]{
			items:    records,
			nextPage: nextPage,
		}, nil
	}
}

// extractNextPage extracts the next page token from API response metadata.
// Returns "" when meta.NextPage is nil (terminal page). Returns an error on
// unknown token types — a silent "" would mask a real pagination bug.
func extractNextPage(meta driftMeta) (string, error) {
	if meta.NextPage == nil {
		return "", nil
	}
	switch v := meta.NextPage.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.Itoa(int(v)), nil
	default:
		return "", fmt.Errorf("drift: unknown next_page token type %T: %v", meta.NextPage, meta.NextPage)
	}
}

// Transform functions - convert API records to model types

func (c *Client) transformTrade(ctx context.Context, record driftTradeRecord, accountUUID uuid.UUID) (*models.TradeInput, *models.PriceRecord, error) {
	isTaker := record.User == record.Taker

	var direction string
	if isTaker {
		direction = record.TakerOrderDirection
	} else {
		direction = record.MakerOrderDirection
	}
	side, err := normalizeSide(direction)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to normalize side for trade %s (ts=%d): %w", record.FillRecordID, record.Ts, err)
	}

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

	marketType := strings.ToLower(record.MarketType)
	if marketType == "" {
		marketType = "perp"
	}

	// Resolve market via index — never trust record.Symbol (Drift renames markets)
	marketInfo, err := c.getMarketInfo(ctx, record.MarketIndex, marketType)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve market index %d (%s): %w", record.MarketIndex, marketType, err)
	}

	baseAsset := marketInfo.BaseAsset
	quoteAsset := marketInfo.QuoteAsset
	baseAmount := cleanDecimalString(record.BaseAssetAmountFilled)
	quoteAmount := cleanDecimalString(record.QuoteAssetAmountFilled)
	price, err := calculatePrice(quoteAmount, baseAmount)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute price for trade %s (base=%q, quote=%q): %w", record.FillRecordID, baseAmount, quoteAmount, err)
	}
	timestamp := time.Unix(record.Ts, 0).UTC()

	trade := &models.TradeInput{
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
		TxSignature:       record.TxSig,
		FeeAsset:          quoteAsset,
	}

	// Extract oracle price if available
	var priceRecord *models.PriceRecord
	if record.OraclePrice != "" && record.OraclePrice != "0" {
		priceRecord = &models.PriceRecord{
			Asset:        baseAsset,
			Denomination: quoteAsset,
			Timestamp:    timestamp,
			Price:        record.OraclePrice,
			Source:       "oracle",
		}
	}

	return trade, priceRecord, nil
}

func (c *Client) transformSwap(ctx context.Context, record driftSwapRecord, accountUUID uuid.UUID) (*models.TradeInput, []*models.PriceRecord, error) {
	// Resolve both sides via market index — never trust record symbols
	outMarket, err := c.getMarketInfo(ctx, record.OutMarketIndex, "spot")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve out market index %d: %w", record.OutMarketIndex, err)
	}
	inMarket, err := c.getMarketInfo(ctx, record.InMarketIndex, "spot")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve in market index %d: %w", record.InMarketIndex, err)
	}

	amountIn := cleanDecimalString(record.AmountIn)
	amountOut := cleanDecimalString(record.AmountOut)
	fee := cleanDecimalString(record.Fee)

	var baseAsset, quoteAsset, side, quantity, price string
	var priceErr error

	// Drift swap semantics: out = what user RECEIVES, in = what user SENDS
	// (from the protocol's perspective: user puts tokens IN, gets tokens OUT)
	if outMarket.BaseAsset == "USDC" {
		// User received USDC (out), sent inMarket asset (in) → sell base for USDC
		baseAsset = inMarket.BaseAsset
		quoteAsset = "USDC"
		side = "sell"
		quantity = amountIn
		price, priceErr = calculatePrice(amountOut, amountIn)
	} else if inMarket.BaseAsset == "USDC" {
		// User sent USDC (in), received outMarket asset (out) → buy base with USDC
		baseAsset = outMarket.BaseAsset
		quoteAsset = "USDC"
		side = "buy"
		quantity = amountOut
		price, priceErr = calculatePrice(amountIn, amountOut)
	} else {
		// Non-USDC swap: user sent inMarket (in), received outMarket (out)
		baseAsset = outMarket.BaseAsset
		quoteAsset = inMarket.BaseAsset
		side = "buy"
		quantity = amountOut
		price, priceErr = calculatePrice(amountIn, amountOut)
	}
	if priceErr != nil {
		return nil, nil, fmt.Errorf("failed to compute price for swap %s_%d (in=%q, out=%q): %w", record.TxSig, record.TxSigIndex, amountIn, amountOut, priceErr)
	}

	if baseAsset == "" || quoteAsset == "" {
		return nil, nil, fmt.Errorf("resolved empty base_asset=%q or quote_asset=%q for swap %s_%d (outIdx=%d, inIdx=%d)",
			baseAsset, quoteAsset, record.TxSig, record.TxSigIndex, record.OutMarketIndex, record.InMarketIndex)
	}

	timestamp := time.Unix(record.Ts, 0).UTC()
	tradeID := fmt.Sprintf("swap_%s_%d", record.TxSig, record.TxSigIndex)

	trade := &models.TradeInput{
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
		TxSignature:       record.TxSig,
		FeeAsset:          quoteAsset,
	}

	// Extract oracle prices for both sides of the swap
	var prices []*models.PriceRecord
	if record.OutOraclePrice != "" && record.OutOraclePrice != "0" {
		prices = append(prices, &models.PriceRecord{
			Asset:        outMarket.BaseAsset,
			Denomination: oracleDenomination,
			Timestamp:    timestamp,
			Price:        record.OutOraclePrice,
			Source:       "oracle",
		})
	}
	if record.InOraclePrice != "" && record.InOraclePrice != "0" {
		prices = append(prices, &models.PriceRecord{
			Asset:        inMarket.BaseAsset,
			Denomination: oracleDenomination,
			Timestamp:    timestamp,
			Price:        record.InOraclePrice,
			Source:       "oracle",
		})
	}

	return trade, prices, nil
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
		ExternalID:        paymentID,
		Metadata: map[string]string{
			"market":     marketInfo.BaseAsset + "-PERP",
			"payment_id": paymentID,
		},
	}, nil
}

func (c *Client) transformDeposit(ctx context.Context, record driftDepositRecord, accountUUID uuid.UUID) (*models.TransferInput, *models.PriceRecord, error) {
	marketInfo, err := c.getMarketInfo(ctx, record.MarketIndex, "spot")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get market info for index %d: %w", record.MarketIndex, err)
	}

	amount := cleanDecimalString(record.Amount)
	timestamp := time.Unix(record.Ts, 0).UTC()

	direction := strings.ToLower(record.Direction)
	transferType := models.TypeDeposit
	if direction == "withdraw" {
		transferType = models.TypeWithdraw
	}

	transfer := &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Asset:             marketInfo.BaseAsset,
		Type:              transferType,
		Amount:            amount,
		Timestamp:         timestamp,
		ExternalID:        record.DepositRecordID,
		Metadata: map[string]string{
			"payment_id": record.DepositRecordID,
		},
	}

	// Extract oracle price if available
	var priceRecord *models.PriceRecord
	if record.OraclePrice != "" && record.OraclePrice != "0" {
		priceRecord = &models.PriceRecord{
			Asset:        marketInfo.BaseAsset,
			Denomination: oracleDenomination,
			Timestamp:    timestamp,
			Price:        record.OraclePrice,
			Source:       "oracle",
		}
	}

	return transfer, priceRecord, nil
}

// Helper functions

// settlementID returns a unique ID for a settlement record.
// Uses TxSig + TxSigIndex when available; falls back to synthetic format.
func settlementID(r settlePnlRecord) string {
	if r.TxSig != "" {
		return fmt.Sprintf("%s_%d", r.TxSig, r.TxSigIndex)
	}
	return fmt.Sprintf("settle_%d_%s", r.MarketIndex, r.Timestamp.Format("20060102150405"))
}

func normalizeSide(direction string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(direction))
	switch normalized {
	case "long":
		return "buy", nil
	case "short":
		return "sell", nil
	default:
		return "", fmt.Errorf("drift: unknown order direction %q", direction)
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

func convertFromBasePrecision(raw string) (string, error) {
	if raw == "" {
		return "0", nil
	}
	return divideByPrecision(raw, BasePrecision)
}

func convertFromQuotePrecision(raw string) (string, error) {
	if raw == "" {
		return "0", nil
	}
	return divideByPrecision(raw, QuotePrecision)
}

func divideByPrecision(raw string, precision int64) (string, error) {
	rawInt := new(big.Int)
	rawInt, ok := rawInt.SetString(raw, 10)
	if !ok {
		return "", fmt.Errorf("drift: failed to parse integer %q for precision division", raw)
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

	return str, nil
}

func calculatePrice(quoteAmount, baseAmount string) (string, error) {
	if baseAmount == "" || baseAmount == "0" {
		return "0", nil
	}

	quoteFloat, _, err := big.ParseFloat(quoteAmount, 10, 256, big.ToNearestEven)
	if err != nil {
		return "", fmt.Errorf("drift: failed to parse quote amount %q: %w", quoteAmount, err)
	}

	baseFloat, _, err := big.ParseFloat(baseAmount, 10, 256, big.ToNearestEven)
	if err != nil {
		return "", fmt.Errorf("drift: failed to parse base amount %q: %w", baseAmount, err)
	}

	if baseFloat.Sign() == 0 {
		return "0", nil
	}

	result := new(big.Float).Quo(quoteFloat, baseFloat)
	str := result.Text('f', 18)

	if strings.Contains(str, ".") {
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	return str, nil
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
