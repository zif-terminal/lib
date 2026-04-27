package hyperliquid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
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
		trade, err := transformFill(fill, accountUUID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to transform fill: %w", err)
		}
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

	// Track hashes of cStakingTransfer entries observed in the non-funding
	// ledger. For some HL wallets the same on-chain consensus staking event
	// surfaces in BOTH userNonFundingLedgerUpdates (as cStakingTransfer) AND
	// delegatorHistory (as cDeposit/cWithdraw). Without dedup we'd emit two
	// transfer rows for one event and double the balance impact.
	stakingHashesFromLedger := make(map[string]bool)

	transfers := make([]*models.TransferInput, 0)
	var prices []*models.PriceRecord
	for _, entry := range entries {
		if strings.EqualFold(entry.Delta.Type, "cStakingTransfer") && entry.Hash != "" {
			stakingHashesFromLedger[entry.Hash] = true
		}
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

	// Fetch borrow/lend interest
	bliEntries, err := c.fetchAllBorrowLendInterest(ctx, user, since)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch borrow/lend interest: %w", err)
	}

	for _, entry := range bliEntries {
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer != nil {
			transfers = append(transfers, transfer)
		}
	}

	// Fetch consensus staking history. cDeposit events (HYPE moving into
	// staking) may not always appear in userNonFundingLedgerUpdates, so we
	// fold delegatorHistory in defensively. When the SAME hash appears in
	// both endpoints we skip the delegatorHistory copy — the ledger path
	// already produced an equivalent transfer and a second insert would
	// hit the unique-on-external_id constraint and abort the whole batch
	// (or, worse, slip in with a different external_id and double the
	// balance impact).
	dhEntries, err := c.fetchAllDelegatorHistory(ctx, user, since)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch delegator history: %w", err)
	}

	for _, entry := range dhEntries {
		// Only cDeposit/cWithdraw deltas could collide with the ledger
		// path; other delegatorHistory event types (delegate, undelegate,
		// withdrawal lifecycle) are dropped inside transformDelegatorHistoryEntry.
		if (entry.Delta.CDeposit != nil || entry.Delta.CWithdraw != nil) &&
			stakingHashesFromLedger[entry.Hash] {
			continue
		}
		transfer, err := transformDelegatorHistoryEntry(entry, accountUUID)
		if err != nil {
			return nil, nil, err
		}
		if transfer != nil {
			transfers = append(transfers, transfer)
		}
	}

	// Sort by timestamp ascending
	sort.Slice(transfers, func(i, j int) bool {
		return transfers[i].Timestamp.Before(transfers[j].Timestamp)
	})

	return transfers, prices, nil
}

// transformDelegatorHistoryEntry converts a Hyperliquid delegatorHistory entry
// into a TransferInput. Only cDeposit and cWithdraw deltas affect the trading
// balance; all other event types (delegate, undelegate, withdrawal lifecycle)
// happen entirely inside consensus staking and are skipped here.
//
// cDeposit  -> TypeWithdraw (HYPE leaving the trading balance into staking)
// cWithdraw -> TypeDeposit  (HYPE returning from staking)
//
// Some HL wallets emit the same on-chain consensus event in BOTH
// delegatorHistory AND userNonFundingLedgerUpdates (as cStakingTransfer).
// FetchDeposits is responsible for dropping any delegatorHistory entry whose
// hash already produced a cStakingTransfer transfer in the same sync — that
// dedup MUST happen before this function is called. The synthetic ExternalID
// here is prefixed (cdeposit_/cwithdraw_) so that for wallets where ONLY
// delegatorHistory reports the event, the row is still distinguishable from
// any future non-staking event that might share the same hash.
func transformDelegatorHistoryEntry(entry hlDelegatorHistoryEntry, accountUUID uuid.UUID) (*models.TransferInput, error) {
	switch {
	case entry.Delta.CDeposit != nil:
		amount := cleanDecimal(entry.Delta.CDeposit.Amount)
		if amount == "" || amount == "0" {
			return nil, fmt.Errorf("hyperliquid: cDeposit entry %s has empty/zero amount %q",
				entry.Hash, entry.Delta.CDeposit.Amount)
		}
		if strings.HasPrefix(amount, "-") {
			amount = amount[1:]
		}
		return &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeWithdraw,
			Asset:             "HYPE",
			Amount:            amount,
			Timestamp:         time.UnixMilli(entry.Time).UTC(),
			ExternalID:        "cdeposit_" + entry.Hash,
			Metadata: map[string]string{
				"payment_id":  entry.Hash,
				"source_type": "cdeposit",
			},
		}, nil

	case entry.Delta.CWithdraw != nil:
		amount := cleanDecimal(entry.Delta.CWithdraw.Amount)
		if amount == "" || amount == "0" {
			return nil, fmt.Errorf("hyperliquid: cWithdraw entry %s has empty/zero amount %q",
				entry.Hash, entry.Delta.CWithdraw.Amount)
		}
		if strings.HasPrefix(amount, "-") {
			amount = amount[1:]
		}
		return &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeDeposit,
			Asset:             "HYPE",
			Amount:            amount,
			Timestamp:         time.UnixMilli(entry.Time).UTC(),
			ExternalID:        "cwithdraw_" + entry.Hash,
			Metadata: map[string]string{
				"payment_id":  entry.Hash,
				"source_type": "cwithdraw",
			},
		}, nil

	default:
		// delegate, undelegate, withdrawal-initiated, withdrawal-finalized,
		// etc. — these don't affect trading balance, ignore.
		return nil, nil
	}
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

	// Combine perp USDC (totalRawUsd) and spot USDC into a single snapshot.
	// Use totalRawUsd (NOT accountValue) — accountValue includes unrealized PnL
	// from open positions, which would cause phantom balance changes every time
	// the market moves.
	perpUSDC, err := strconv.ParseFloat(perpState.MarginSummary.TotalRawUsd, 64)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: failed to parse perp totalRawUsd %q: %w", perpState.MarginSummary.TotalRawUsd, err)
	}

	spotUSDC := 0.0
	for _, b := range spotState.Balances {
		if b.Coin == "USDC" {
			spotUSDC, err = strconv.ParseFloat(b.Total, 64)
			if err != nil {
				return nil, fmt.Errorf("hyperliquid: failed to parse spot USDC balance %q: %w", b.Total, err)
			}
			break
		}
	}

	totalUSDC := perpUSDC + spotUSDC
	if math.Abs(totalUSDC) > 0.000001 {
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       "USDC",
			Balance:     cleanDecimal(strconv.FormatFloat(totalUSDC, 'f', -1, 64)),
			TimestampMs: nowMs,
		})
	}

	// Add non-USDC spot balances
	for _, b := range spotState.Balances {
		if b.Coin == "USDC" {
			continue // already combined above
		}
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
//
// Returns an error if the fill is malformed — in particular, if it carries a
// non-zero fee with an empty feeToken. Hyperliquid spot fees are denominated in
// the asset RECEIVED (e.g., USDH on USDH buys, USDC on sells); blindly defaulting
// to USDC produces phantom balance deltas in the processor, so we refuse to
// guess and fail loudly instead.
func transformFill(fill hlFill, accountUUID uuid.UUID) (*models.TradeInput, error) {
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

	feeAsset, err := deriveFeeAsset(fill)
	if err != nil {
		return nil, err
	}

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
		FeeAsset:          feeAsset,
	}, nil
}

// deriveFeeAsset returns the fee denomination for a fill, enforcing that every
// non-zero-fee fill MUST carry a feeToken. Hyperliquid spot fills charge fees in
// the asset RECEIVED (USDH on USDH buys, USDC on sells), which is why we must
// honour feeToken rather than hardcoding USDC.
//
// Fails loudly on malformed fills (non-zero fee with empty feeToken) instead of
// silently defaulting — a silent default is exactly how the original bug
// produced phantom USDH/USDC positions.
func deriveFeeAsset(fill hlFill) (string, error) {
	token := strings.ToUpper(strings.TrimSpace(fill.FeeToken))
	fee := cleanDecimal(fill.Fee)
	if token == "" {
		// A zero fee with no token is acceptable (e.g., synthetic opens, some
		// pre-fee-token fills), but still needs a deterministic label. Default
		// to USDC per HL perp convention — safe because the fee is 0 so the
		// token is purely a label, never used in balance math.
		if fee == "" || fee == "0" || fee == "-0" {
			return "USDC", nil
		}
		return "", fmt.Errorf("hyperliquid fill %d has non-zero fee %s but empty feeToken — refusing to default", fill.Tid, fill.Fee)
	}
	return token, nil
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
		// Defensive: HL deposits typically have fee=0, but if a non-zero fee
		// is ever reported we want it folded in (and logged so we can diagnose).
		// We treat deposit fee as an additional inflow reduction: actual wallet
		// delta into HL is (usdc - fee) from the user's perspective. Using
		// absolute-value arithmetic via addSignedDecimals handles either sign.
		amountStr = foldLedgerFee(amountStr, entry.Delta.Fee, deltaType, entry.Hash)

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
		// Fold HL bridge fee into the withdraw amount so the transfer row
		// matches the user's actual wallet-level cashflow. HL ledger encodes
		// withdraws as {"type":"withdraw","usdc":"47999","fee":"1.0"} — the
		// user is out 48,000 total. Without this, we'd record a 47,999
		// debit and leave a phantom +fee residual on the account.
		amountStr = foldLedgerFee(amountStr, entry.Delta.Fee, deltaType, entry.Hash)

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
		// Skip self-sends — wallet sending to itself is a no-op on chain but
		// would otherwise be recorded as a deposit (since destination == wallet),
		// double-crediting the account.
		if entry.Delta.User != "" && entry.Delta.Destination != "" &&
			strings.EqualFold(entry.Delta.User, entry.Delta.Destination) {
			return nil, nil, nil
		}
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

	case "internaltransfer", "subaccounttransfer":
		if entry.Delta.Usdc == "" {
			return nil, nil, fmt.Errorf("hyperliquid %s ledger entry %s missing usdc field", deltaType, entry.Hash)
		}
		if entry.Delta.User == "" {
			return nil, nil, fmt.Errorf("hyperliquid %s ledger entry %s missing user field", deltaType, entry.Hash)
		}
		if entry.Delta.Destination == "" {
			return nil, nil, fmt.Errorf("hyperliquid %s ledger entry %s missing destination field", deltaType, entry.Hash)
		}
		if strings.EqualFold(entry.Delta.Destination, walletAddress) {
			transferType = models.TypeDeposit
		} else if strings.EqualFold(entry.Delta.User, walletAddress) {
			transferType = models.TypeWithdraw
		} else {
			return nil, nil, fmt.Errorf("hyperliquid %s ledger entry %s neither user (%s) nor destination (%s) matches account wallet (%s)",
				deltaType, entry.Hash, entry.Delta.User, entry.Delta.Destination, walletAddress)
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

	case "cstakingtransfer":
		// HyperStake consensus staking. Staking locks tokens out of the trading
		// balance; unstaking returns them. Direction is indicated by isDeposit.
		if entry.Delta.IsDeposit {
			transferType = models.TypeWithdraw
		} else {
			transferType = models.TypeDeposit
		}
		asset = entry.Delta.Token
		amountStr = entry.Delta.Amount

	case "accountclasstransfer":
		// Internal rebalance between the user's own spot and perp sub-accounts.
		// This is NOT a cashflow — the wallet's total USDC position is unchanged,
		// just moved between trading classes. Intentionally skipped.
		return nil, nil, nil

	case "vaultwithdraw":
		transferType = models.TypeDeposit
		asset = "USDC"
		amountStr = entry.Delta.NetWithdrawnUsd

	case "vaultleadercommission":
		transferType = models.TypeReward
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "rewardsclaim":
		transferType = models.TypeReward
		asset = "USDC"
		amountStr = entry.Delta.Usdc

	case "send":
		// Skip self-sends — when user == destination the event is a no-op on
		// chain, but our deposit/withdraw direction logic (based on which side
		// matches walletAddress) would otherwise double-credit or mis-handle it.
		if strings.EqualFold(entry.Delta.User, entry.Delta.Destination) {
			return nil, nil, nil
		}
		incoming := strings.EqualFold(entry.Delta.Destination, walletAddress)
		if incoming {
			transferType = models.TypeDeposit
		} else {
			transferType = models.TypeWithdraw
		}
		if entry.Delta.Token != "" {
			asset = entry.Delta.Token
			amountStr = entry.Delta.Amount
			// Derive price from usdcValue / amount when available (same as spotTransfer)
			if asset != "USDC" && entry.Delta.UsdcValue != "" && entry.Delta.UsdcValue != "0" {
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
		} else {
			asset = "USDC"
			amountStr = entry.Delta.Usdc
		}
		// Fold the HL transfer fee into the amount for OUTGOING sends only.
		// HL reports the fee on both sides of the ledger, but the fee is
		// debited from the sender's wallet — so for incoming sends the
		// recipient's credit already reflects the net (fee was paid by the
		// other party). For outgoing sends the user is out (amount + fee);
		// without this fold we'd under-record the debit and leave a phantom
		// fee-sized residual on the account.
		if !incoming {
			amountStr = foldLedgerFee(amountStr, entry.Delta.Fee, deltaType, entry.Hash)
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

	// Some ledger entries share the same hash (e.g. vaultWithdraw and
	// vaultLeaderCommission for the same vault withdrawal). Disambiguate
	// by appending the delta type for types that are known to collide.
	externalID := entry.Hash
	if deltaType == "vaultwithdraw" {
		externalID = entry.Hash + "_" + deltaType
	}

	metadata := map[string]string{
		"payment_id":  entry.Hash,
		"source_type": deltaType,
	}

	// Add vault address to metadata for vault-related entries
	if entry.Delta.Vault != "" {
		switch deltaType {
		case "vaultcreate", "vaultdeposit", "vaultwithdraw", "vaultdistribution", "vaultleadercommission":
			metadata["vault_address"] = entry.Delta.Vault
		}
	}

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              transferType,
		Asset:             asset,
		Amount:            amount,
		Timestamp:         time.UnixMilli(entry.Time).UTC(),
		ExternalID:        externalID,
		Metadata:          metadata,
	}, priceRecord, nil
}

// transformBorrowLendInterest converts a Hyperliquid borrow/lend interest entry to a TransferInput.
// Net interest = supply - borrow. Positive = earned, negative = paid. Zero net is skipped.
// Uses math/big.Rat for exact decimal arithmetic.
func transformBorrowLendInterest(entry hlBorrowLendInterest, accountUUID uuid.UUID) *models.TransferInput {
	supply := new(big.Rat)
	if _, ok := supply.SetString(entry.Supply); !ok {
		supply.SetInt64(0)
	}

	borrow := new(big.Rat)
	if _, ok := borrow.SetString(entry.Borrow); !ok {
		borrow.SetInt64(0)
	}

	// net = supply - borrow
	net := new(big.Rat).Sub(supply, borrow)

	if net.Sign() == 0 {
		return nil
	}

	// Format the net amount as a decimal string
	amount := net.FloatString(18)
	amount = cleanDecimal(amount)

	// Make amount positive — direction is determined by type
	if strings.HasPrefix(amount, "-") {
		amount = amount[1:]
	}

	// If after cleaning the amount is zero or dust, skip
	if amount == "0" {
		return nil
	}

	externalID := fmt.Sprintf("bli_%s_%d", entry.Token, entry.Time)

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeInterest,
		Asset:             entry.Token,
		Amount:            amount,
		Timestamp:         time.UnixMilli(entry.Time).UTC(),
		ExternalID:        externalID,
		Metadata: map[string]string{
			"source_type": "borrow_lend_interest",
			"borrow":      entry.Borrow,
			"supply":      entry.Supply,
		},
	}
}

// foldLedgerFee adds the ledger entry's Fee into the base amount (in absolute
// terms) so that the resulting transfer row reflects the user's actual
// wallet-level cashflow, not just the net that landed on the exchange.
//
// HL withdraws are reported as {usdc: "47999", fee: "1.0"} meaning the user
// was debited 48,000 total (47,999 delivered + 1 bridge fee). Recording only
// 47,999 leaves a phantom fee-sized USDC residual on the account.
//
// For deposits, HL typically reports fee="0"; this function is applied
// defensively and logs a warning if it ever sees a non-zero deposit fee so
// we can diagnose. The fold direction is the same (|amount| + |fee|) —
// conservative by always attributing the fee to the user's wallet-side
// outflow (which is the semantically meaningful cashflow).
//
// The sign of `base` is preserved so downstream absolute-value normalisation
// continues to work unchanged. Empty/zero fees are a no-op.
func foldLedgerFee(base, fee, deltaType, hash string) string {
	feeRat := new(big.Rat)
	if fee == "" {
		return base
	}
	if _, ok := feeRat.SetString(fee); !ok {
		// Unparseable fee — leave base untouched; this matches the
		// permissive style of cleanDecimal elsewhere.
		return base
	}
	if feeRat.Sign() == 0 {
		return base
	}

	baseRat := new(big.Rat)
	if base != "" {
		if _, ok := baseRat.SetString(base); !ok {
			return base
		}
	}

	// Use absolute magnitudes for both operands, then reapply base's sign.
	// This keeps behaviour consistent regardless of how HL signs usdc/fee.
	absBase := new(big.Rat).Abs(baseRat)
	absFee := new(big.Rat).Abs(feeRat)
	sum := new(big.Rat).Add(absBase, absFee)

	if deltaType == "deposit" {
		log.Printf("hyperliquid/ledger: deposit with non-zero fee observed — folding fee into amount | hash=%s base=%s fee=%s folded=%s",
			hash, base, fee, sum.FloatString(18))
	}

	if baseRat.Sign() < 0 {
		sum.Neg(sum)
	}

	return sum.FloatString(18)
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
		// FeeToken is set explicitly (USDC) to satisfy the deriveFeeAsset
		// invariant that every fill carries a fee denomination. The fee itself
		// is zero so the token is purely a label.
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
			FeeToken:      "USDC",
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
