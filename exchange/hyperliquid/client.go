package hyperliquid

import (
	"bytes"
	"context"
	"crypto/sha256"
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

			// Respect Retry-After header if present
			if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > 0 {
				backoff = retryAfter
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

	// Synthesize opening fills for pre-launch perp positions.
	// Pre-launch perps (e.g., @107) may only have closing fills in the API —
	// the position was opened via pre-launch auction/airdrop and never appears as a fill.
	// We detect this by looking at startPosition on the earliest fill per coin:
	// if startPosition > 0 and there's no earlier buy fill, we synthesize an opening
	// fill at ~$0 cost basis (derived from closedPnl) to give the processor a complete
	// position lifecycle.
	fills = synthesizePrelaunchOpenings(fills, accountUUID)

	trades := make([]*models.TradeInput, 0, len(fills))
	prices := make([]*models.PriceRecord, 0, len(fills))
	for _, fill := range fills {
		trade := transformFill(fill, accountUUID)
		if err := c.resolveSpotTradeNames(ctx, trade); err != nil {
			return nil, nil, fmt.Errorf("failed to resolve spot coin name for %q: %w", fill.Coin, err)
		}
		trades = append(trades, trade)

		// Build price record from execution price (must be done before sorting trades)
		if fill.Px != "" && fill.Px != "0" {
			prices = append(prices, &models.PriceRecord{
				Asset:        trade.BaseAsset,
				Denomination: trade.QuoteAsset,
				Timestamp:    trade.Timestamp,
				Price:        cleanDecimal(fill.Px),
				Source:       "execution",
			})
		}
	}

	// Sort by timestamp ascending
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	return trades, prices, nil
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
	var prices []*models.PriceRecord
	for _, entry := range entries {
		transfer, price, err := transformLedgerEntry(entry, accountUUID, user)
		if err != nil {
			return nil, nil, err
		}
		if transfer != nil {
			transfers = append(transfers, transfer)
		}
		if price != nil {
			prices = append(prices, price)
		}
	}

	// Sort by timestamp ascending
	sort.Slice(transfers, func(i, j int) bool {
		return transfers[i].Timestamp.Before(transfers[j].Timestamp)
	})

	return transfers, prices, nil
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
	accountValue, err := strconv.ParseFloat(perpState.MarginSummary.AccountValue, 64)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: failed to parse perp account value %q: %w", perpState.MarginSummary.AccountValue, err)
	}
	if math.Abs(accountValue) > 0.000001 {
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       "USDC",
			Balance:     perpState.MarginSummary.AccountValue,
			TimestampMs: nowMs,
		})
	}

	// Add spot balances
	for _, b := range spotState.Balances {
		total, err := strconv.ParseFloat(b.Total, 64)
		if err != nil {
			return nil, fmt.Errorf("hyperliquid: failed to parse spot balance %q for coin %s: %w", b.Total, b.Coin, err)
		}
		if math.Abs(total) < 0.000001 {
			continue
		}
		asset := b.Coin
		// Resolve @N coin names to real token names
		if len(asset) > 0 && asset[0] == '@' {
			base, _, resolveErr := c.resolveSpotCoin(ctx, asset)
			if resolveErr != nil {
				return nil, fmt.Errorf("hyperliquid: failed to resolve spot coin %q to asset symbol: %w", asset, resolveErr)
			}
			asset = base
		}
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       asset,
			Balance:     b.Total,
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

// resolveSpotTradeNames resolves @N base asset names in spot trades to real token names
// using the spot metadata cache. No-op for perp trades or trades that already have resolved names.
func (c *Client) resolveSpotTradeNames(ctx context.Context, trade *models.TradeInput) error {
	if trade.MarketType != "spot" {
		return nil
	}
	if len(trade.BaseAsset) == 0 || trade.BaseAsset[0] != '@' {
		return nil
	}
	base, quote, err := c.resolveSpotCoin(ctx, trade.BaseAsset)
	if err != nil {
		return err
	}
	trade.BaseAsset = base
	trade.QuoteAsset = quote
	return nil
}

// transformFill converts a Hyperliquid fill to a TradeInput.
// Note: spot fills with @N coin names will have BaseAsset="@N" after this call.
// Call resolveSpotTradeNames to resolve them to real token names.
func transformFill(fill hlFill, accountUUID uuid.UUID) *models.TradeInput {
	coin := fill.Coin
	marketType := "perp"
	baseAsset := coin

	// Spot Dust Conversions are spot sells of tiny balances, not perp trades.
	// Hyperliquid auto-converts dust token balances to USDC with dir="Spot Dust Conversion".
	// These fills use @N coin names without -SPOT suffix, so they'd be misclassified as perp.
	// The matching opening position comes from spotGenesis ledger entries (deposit transfers).
	if fill.Dir == "Spot Dust Conversion" {
		marketType = "spot"
	}

	// Spot coins have a "-SPOT" suffix in Hyperliquid
	if strings.HasSuffix(coin, "-SPOT") {
		marketType = "spot"
		baseAsset = strings.TrimSuffix(coin, "-SPOT")
	}

	// Bare @N coins are spot tokens per the HL API spec (e.g., @107 for HYPE).
	// Perps use plain symbol names from the meta response (e.g., "ETH", "BTC").
	if strings.HasPrefix(coin, "@") {
		marketType = "spot"
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

	tradeID := strconv.FormatInt(fill.Tid, 10)
	if fill.Tid == 0 {
		h := fmt.Sprintf("%d|%s|%s|%s|%s|%d", fill.Time, fill.Coin, fill.Side, fill.Px, fill.Sz, fill.Oid)
		tradeID = fmt.Sprintf("hl_%x", sha256.Sum256([]byte(h)))
	}

	return &models.TradeInput{
		TradeID:           tradeID,
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
		FeeAsset:          "USDC",
	}
}

// transformFunding converts a Hyperliquid funding entry to a TransferInput.
func transformFunding(entry hlFundingEntry, accountUUID uuid.UUID) *models.TransferInput {
	externalID := fmt.Sprintf("%d_%s", entry.Time, entry.Delta.Coin)
	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeFunding,
		Asset:             "USDC",
		Amount:            cleanDecimal(entry.Delta.Usdc), // Keep signed — Drift also stores signed funding amounts
		Timestamp:         time.UnixMilli(entry.Time).UTC(),
		ExternalID:        externalID,
		Metadata: map[string]string{
			"market":      entry.Delta.Coin + "-PERP",
			"funding_rate": entry.Delta.FundingRate,
			"n_samples":   strconv.Itoa(entry.Delta.NSamples),
			"payment_id":  externalID,
		},
	}
}

// transformLedgerEntry converts a Hyperliquid ledger entry to a TransferInput and optional PriceRecord.
// Returns (nil, nil, nil) for intentionally-skipped entry types (e.g. accountClassTransfer).
// Returns an error for unknown delta types — we must not silently drop cashflows.
// walletAddress is needed to determine direction for spotTransfer and internalTransfer.
func transformLedgerEntry(entry hlLedgerEntry, accountUUID uuid.UUID, walletAddress string) (*models.TransferInput, *models.PriceRecord, error) {
	deltaType := strings.ToLower(entry.Delta.Type)

	var transferType string
	var asset string
	var amountStr string
	var priceRecord *models.PriceRecord

	switch deltaType {
	case "deposit":
		transferType = models.TypeDeposit
		asset = "USDC"
		amountStr = entry.Delta.Usdc
		if amountStr == "" {
			amountStr = entry.Delta.Amount
		}
		if entry.Delta.Token != "" {
			asset = entry.Delta.Token
		}

	case "withdraw":
		transferType = models.TypeWithdraw
		asset = "USDC"
		amountStr = entry.Delta.Usdc
		if amountStr == "" {
			amountStr = entry.Delta.Amount
		}
		if entry.Delta.Token != "" {
			asset = entry.Delta.Token
		}

	case "spotgenesis":
		transferType = models.TypeDeposit
		asset = entry.Delta.Token
		amountStr = entry.Delta.Amount
		// spotGenesis entries are free airdrops — price is 0
		priceRecord = &models.PriceRecord{
			Asset:        entry.Delta.Token,
			Denomination: "USDC",
			Timestamp:    time.UnixMilli(entry.Time).UTC(),
			Price:        "0",
			Source:       "ledger",
		}

	case "spottransfer":
		if strings.EqualFold(entry.Delta.Destination, walletAddress) {
			transferType = models.TypeDeposit
		} else {
			transferType = models.TypeWithdraw
		}
		asset = entry.Delta.Token
		amountStr = entry.Delta.Amount
		// Derive price from usdcValue / amount when available
		if entry.Delta.UsdcValue != "" {
			usdcVal, uErr := strconv.ParseFloat(entry.Delta.UsdcValue, 64)
			amt, aErr := strconv.ParseFloat(entry.Delta.Amount, 64)
			if uErr == nil && aErr == nil && amt != 0 {
				price := math.Abs(usdcVal / amt)
				priceRecord = &models.PriceRecord{
					Asset:        entry.Delta.Token,
					Denomination: "USDC",
					Timestamp:    time.UnixMilli(entry.Time).UTC(),
					Price:        cleanDecimal(strconv.FormatFloat(price, 'f', -1, 64)),
					Source:       "ledger",
				}
			}
		}

	case "internaltransfer":
		if strings.EqualFold(entry.Delta.Destination, walletAddress) {
			transferType = models.TypeDeposit
		} else {
			transferType = models.TypeWithdraw
		}
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "vaultcreate", "vaultdeposit":
		transferType = models.TypeWithdraw
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "vaultdistribution":
		transferType = models.TypeDeposit
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "accountclasstransfer":
		// Internal rebalance between the user's own spot and perp sub-accounts.
		// This is NOT a cashflow — the wallet's total USDC position is unchanged,
		// just moved between trading classes. Intentionally skipped.
		return nil, nil, nil

	case "vaultwithdraw":
		// Skip until we understand the full vault withdrawal flow
		return nil, nil, nil

	case "vaultleadercommission":
		transferType = models.TypeReward
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "rewardsclaim":
		transferType = models.TypeReward
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "send":
		if strings.EqualFold(entry.Delta.Destination, walletAddress) {
			transferType = models.TypeDeposit
		} else {
			transferType = models.TypeWithdraw
		}
		if entry.Delta.Token != "" {
			asset = entry.Delta.Token
			amountStr = entry.Delta.Amount
		} else {
			asset = "USDC"
			amountStr = entry.Delta.Usdc
		}

	case "liquidation":
		// Informational only — no cash flow. Intentionally skipped.
		return nil, nil, nil

	default:
		return nil, nil, fmt.Errorf("hyperliquid: unknown ledger delta type %q for entry at timestamp=%d hash=%s", deltaType, entry.Time, entry.Hash)
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
		ExternalID:        entry.Hash,
		Metadata: map[string]string{
			"payment_id":  entry.Hash,
			"source_type": deltaType,
		},
	}, priceRecord, nil
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

// synthesizePrelaunchOpenings detects fills where the API only returns closing/sell
// fills but no corresponding opening/buy fills. This happens in two cases:
//
//  1. Pre-launch perp positions (e.g., @107): opened via pre-launch auction, only
//     close fills appear in the API.
//  2. Spot dust conversions (e.g., @2 with dir="Spot Dust Conversion"): airdrop tokens
//     auto-converted to USDC. The tokens were received via airdrop, not a trade.
//
// For each such coin, this function inserts a synthetic opening buy fill 1ms before
// the earliest real fill, with qty = startPosition and price = $0 (airdrop cost basis).
// This gives the processor a complete position lifecycle (open → close).
func synthesizePrelaunchOpenings(fills []hlFill, accountUUID uuid.UUID) []hlFill {
	// Group fills by coin, track earliest fill per coin
	type coinInfo struct {
		earliestIdx int
		earliestMs  int64
		hasBuy      bool
	}
	coins := make(map[string]*coinInfo)

	for i, fill := range fills {
		coin := fill.Coin
		// Skip spot fills — their opening positions come from deposit transfers, not pre-launch auctions.
		// Spot fills use -SPOT suffix, BASE/QUOTE format, or bare @N coin names.
		if strings.HasSuffix(coin, "-SPOT") || strings.Contains(coin, "/") || strings.HasPrefix(coin, "@") {
			continue
		}

		info, ok := coins[coin]
		if !ok {
			info = &coinInfo{earliestIdx: i, earliestMs: fill.Time}
			coins[coin] = info
		}
		if fill.Time < info.earliestMs {
			info.earliestIdx = i
			info.earliestMs = fill.Time
		}
		if fill.Side == "B" {
			info.hasBuy = true
		}
	}

	var synthetic []hlFill
	for coin, info := range coins {
		if info.hasBuy {
			continue // Has a real buy — no synthesis needed
		}
		earliest := fills[info.earliestIdx]
		startPos := cleanDecimal(earliest.StartPosition)
		if startPos == "0" || startPos == "" {
			continue // No pre-existing position
		}

		// Skip dust conversions — their opening positions come from spotGenesis
		// ledger entries (captured as deposit transfers in FetchDeposits).
		// Synthesizing a fill here would double-count the inflow.
		if earliest.Dir == "Spot Dust Conversion" {
			continue
		}

		// Synthesize an opening buy fill 1ms before the earliest real fill.
		// Price = $0 (pre-launch airdrop/auction cost basis is effectively zero).
		// TradeID uses a deterministic negative TID so it's unique and reproducible.
		synthetic = append(synthetic, hlFill{
			Time:          earliest.Time - 1,
			Coin:          coin,
			Side:          "B", // buy to open long
			Px:            "0",
			Sz:            startPos,
			Fee:           "0",
			Tid:           -earliest.Time, // deterministic unique ID
			ClosedPnl:     "0",
			Hash:          "synthetic_prelaunch_open_" + coin,
			StartPosition: "0",
			Dir:           "Synthetic Open Long",
			Oid:           0,
		})
	}

	if len(synthetic) == 0 {
		return fills
	}

	return append(synthetic, fills...)
}

// parseRetryAfter parses the Retry-After header value (in seconds) into a duration.
// Returns 0 if the header is empty or unparseable.
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

// Compile-time interface check
var _ iface.ExchangeClient = (*Client)(nil)
