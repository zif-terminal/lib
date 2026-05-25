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
	"github.com/zif-terminal/lib/exchange/httpcache"
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
	transport  httpcache.Transport
	// principal scopes the httpcache key per-wallet. Hyperliquid POSTs are
	// never cached (per package spec); routing through the transport is
	// still useful so future read-only GETs can opt in uniformly.
	principal string
	// dbClient is optional. When non-nil the fetch methods read
	// manual_adjustments rows for the account and fold them into the
	// real-data feed before transformers run. The legacy NewClient() path
	// leaves it nil so unit tests that don't need adjustments don't have to
	// spin up a fake DB. See lib/exchange/hyperliquid/manual_adjustments.go.
	dbClient adjustmentsReader
}

// NewClient creates a new Hyperliquid client without database access.
// Callers that want to fold manual_adjustments into the sync feed should
// use NewClientWithDB.
func NewClient() *Client {
	tr, err := httpcache.NewFromEnv()
	if err != nil {
		log.Printf("hyperliquid: httpcache disabled (%v) — falling back to passthrough", err)
		tr = httpcache.Passthrough()
	}
	return &Client{
		apiURL:     baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		transport:  tr,
	}
}

// NewClientWithDB creates a Hyperliquid client that reads from
// manual_adjustments for each account at fetch time. Each adjustment is
// merged with the real-data feed before per-event-type transformers run,
// so adjustments and real events share the exact same code path and the
// resulting Transfer rows are tagged with metadata.manual_adjustment_id.
func NewClientWithDB(dbClient adjustmentsReader) *Client {
	c := NewClient()
	c.dbClient = dbClient
	return c
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "hyperliquid"
}

// doPost sends a POST request to the Hyperliquid info API and decodes the response.
// Hyperliquid's read API is POST-only, so by current spec these calls are never
// cached — the transport always delegates to the live HTTP path. Routing through
// the transport here keeps the per-exchange wiring uniform.
func (c *Client) doPost(ctx context.Context, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.transport != nil && c.transport.IsEnabled() && c.principal != "" {
		respBody, err := c.transport.Post(ctx, "hyperliquid", c.apiURL, c.principal, data, func(ctx context.Context) ([]byte, error) {
			return c.livePost(ctx, data)
		})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
		return nil
	}

	respBody, err := c.livePost(ctx, data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// livePost performs the live POST with rate-limit + retry, returning the raw
// body bytes. Separate from doPost so the cache transport can use it as its
// upstream Fetcher without re-entering the cache.
func (c *Client) livePost(ctx context.Context, data []byte) ([]byte, error) {
	backoff := initialBackoff
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if err := globalLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		// Check for rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()

			if attempt == maxRetries {
				return nil, &iface.RateLimitError{
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
				return nil, ctx.Err()
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
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
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

	// Merge active manual_adjustments of event_type=userFills into the fill
	// stream. Adjustment-sourced fills pass through transformFill identically
	// to real ones — the only audit trail downstream is the Tid (Trade ID)
	// the operator put in the payload (TradeInput has no metadata column, so
	// metadata.manual_adjustment_id is NOT carried on trade rows). For Phase A
	// only HL funding is exercised end-to-end; the userFills code path is
	// wired but unverified.
	adjByType, err := c.fetchAdjustmentsByType(ctx, accountUUID)
	if err != nil {
		return nil, nil, err
	}
	fillAdj, _, err := decodeFillsAdjustments(adjByType["userFills"])
	if err != nil {
		return nil, nil, err
	}
	fills = append(fills, fillAdj...)

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

	// Merge active manual_adjustments of event_type=userFunding into the
	// entry stream BEFORE running transformFunding. Each adjustment's
	// payload decodes as an hlFundingEntry so it flows through the same
	// transformer as the real entries.
	adjByType, err := c.fetchAdjustmentsByType(ctx, accountUUID)
	if err != nil {
		return nil, err
	}
	fundingAdj, adjSourceIDs, err := decodeFundingAdjustments(adjByType["userFunding"])
	if err != nil {
		return nil, err
	}
	// Track the adjustment_id for each merged entry by its external_id so
	// we can tag the resulting Transfer row after transformation. We use
	// external_id as the join key because transformFunding's externalID
	// derivation is deterministic from entry fields (time + coin).
	adjExtIDToSource := make(map[string]uuid.UUID, len(fundingAdj))
	for i := range fundingAdj {
		extID := fundingExternalID(fundingAdj[i])
		adjExtIDToSource[extID] = adjSourceIDs[i]
		entries = append(entries, fundingAdj[i])
	}

	payments := make([]*models.TransferInput, 0, len(entries))
	for _, entry := range entries {
		payment := transformFunding(entry, accountUUID)
		// If this entry came from a manual_adjustment, tag the resulting
		// Transfer so the audit trail survives into the DB.
		if adjID, ok := adjExtIDToSource[payment.ExternalID]; ok {
			if payment.Metadata == nil {
				payment.Metadata = make(map[string]string)
			}
			payment.Metadata["manual_adjustment_id"] = adjID.String()
			payment.Metadata["source"] = "manual_adjustment"
		}
		payments = append(payments, payment)
	}

	// Sort by timestamp ascending
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Timestamp.Before(payments[j].Timestamp)
	})

	return payments, nil
}

// fundingExternalID rebuilds the externalID that transformFunding will
// stamp on the resulting TransferInput. Defined here so the merge code
// can pre-compute the join key without calling the transformer twice.
func fundingExternalID(entry hlFundingEntry) string {
	return fmt.Sprintf("%d_%s", entry.Time, entry.Delta.Coin)
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

	// Merge active manual_adjustments of event_type=userNonFundingLedgerUpdates
	// into the entry stream BEFORE running transformLedgerEntry. Adjustment
	// payloads decode as hlLedgerEntry so they flow through the same
	// transformer as real entries. Tag the resulting Transfer rows with
	// metadata.manual_adjustment_id via a hash→adjustment_id map.
	adjByType, err := c.fetchAdjustmentsByType(ctx, accountUUID)
	if err != nil {
		return nil, nil, err
	}
	ledgerAdj, ledgerAdjSources, err := decodeLedgerAdjustments(adjByType["userNonFundingLedgerUpdates"])
	if err != nil {
		return nil, nil, err
	}
	hashToAdjID := make(map[string]uuid.UUID, len(ledgerAdj))
	for i := range ledgerAdj {
		if ledgerAdj[i].Hash == "" {
			return nil, nil, fmt.Errorf("manual_adjustment %s of type userNonFundingLedgerUpdates has empty hash (must be a unique synthetic hash, e.g. 0x_manual_<reason>)", ledgerAdjSources[i])
		}
		hashToAdjID[ledgerAdj[i].Hash] = ledgerAdjSources[i]
		entries = append(entries, ledgerAdj[i])
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
			tagWithAdjustment(transfer, entry.Hash, hashToAdjID)
			transfers = append(transfers, transfer)
		}
		if price != nil {
			prices = append(prices, price)
		}
		// Emit a separate USDC fee withdraw for entry types that charge a
		// USDC-denominated bridge fee on a non-USDC asset move (send /
		// spotTransfer of a non-USDC token). Folding the USDC fee into the
		// token amount produces a phantom position in that token.
		if feeTransfer := extraLedgerFeeTransfer(entry, accountUUID, user); feeTransfer != nil {
			tagWithAdjustment(feeTransfer, entry.Hash, hashToAdjID)
			transfers = append(transfers, feeTransfer)
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
		// path; the staking-internal lifecycle variants (delegate,
		// undelegate, withdrawal-queue phases) are explicitly no-op'd
		// inside transformDelegatorHistoryEntry, and anything genuinely
		// unknown still crash-louds there.
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

	case entry.Delta.Delegate != nil:
		// delegate/undelegate: HYPE moving between the staking account and a
		// validator. Entirely staking-internal — never touches the
		// spot/trading balance. Explicit no-op (no transfer, no error) per
		// no-silent-unknowns policy.
		return nil, nil

	case entry.Delta.Withdrawal != nil:
		// Unstaking-queue lifecycle marker (phase initiated|finalized). The
		// real spot credit arrives via the separate cWithdraw /
		// cStakingTransfer{isDeposit:false} path; emitting here too would
		// double-count. Neither phase moves the trading balance — explicit
		// no-op for both.
		return nil, nil

	default:
		// Genuinely unknown delegator-history variant. The raw JSON's
		// top-level key names the type (e.g. {"cValidatorActivation":{...}}).
		// Crash-loud so the new type gets an explicit case rather than being
		// silently dropped — silent skips were the root-cause class for the
		// Lighter USDC phantoms we spent hours chasing. RawDelta is captured
		// by hlDelegatorHistoryEntry.UnmarshalJSON.
		return nil, fmt.Errorf("hyperliquid: unhandled delegator history variant | time=%d | hash=%s | raw_delta=%s",
			entry.Time, entry.Hash, string(entry.RawDelta))
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

	// Fetch per-asset spot mark prices once. Used to attach oracle_price +
	// usd_value to every non-USDC balance row below. This call is what closes
	// the spot_balance_snapshots usd_value=NULL bug — without it dashboard
	// summed-USD reconciliations under-count any account holding HYPE/UBTC/
	// HAR/etc as collateral.
	spotPrices, err := c.fetchSpotMarkPrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spot mark prices: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	var balances []*models.BalanceSnapshot

	// Combine perp USDC and spot USDC into a single snapshot.
	//
	// We can't simply use totalRawUsd: under cross-margin, HL's totalRawUsd
	// includes the mark-to-market notional of spot tokens supplied as
	// collateral (e.g. HYPE pledged against perp positions). Those same spot
	// tokens are ALSO reported as separate spot balances, which would
	// double-count their value into USDC.
	//
	// Instead: derive realized perp cash as
	//     accountValue − Σ(unrealizedPnl across assetPositions)
	// accountValue = total perp wallet equity (cash + unrealized PnL on
	// positions, in USD terms). Subtracting unrealized PnL leaves only the
	// realized USDC cash sitting in the perp wallet, with no contribution
	// from open-position MTM. We do NOT use totalRawUsd because for
	// cross-margin accounts it is inflated by supplied-collateral notional.
	accountValue, err := strconv.ParseFloat(perpState.MarginSummary.AccountValue, 64)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: failed to parse perp accountValue %q: %w", perpState.MarginSummary.AccountValue, err)
	}
	perpUSDC := accountValue
	for _, ap := range perpState.AssetPositions {
		if ap.Position.UnrealizedPnl == "" {
			continue
		}
		unr, err := strconv.ParseFloat(ap.Position.UnrealizedPnl, 64)
		if err != nil {
			return nil, fmt.Errorf("hyperliquid: failed to parse unrealizedPnl %q for coin %s: %w", ap.Position.UnrealizedPnl, ap.Position.Coin, err)
		}
		perpUSDC -= unr
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
		usdcBalance := cleanDecimal(strconv.FormatFloat(totalUSDC, 'f', -1, 64))
		one := "1"
		// USDC's USD value is its balance (price = 1.0 by definition).
		usdcVal := usdcBalance
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       "USDC",
			Balance:     usdcBalance,
			OraclePrice: &one,
			UsdValue:    &usdcVal,
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

		// Look up mark price (keyed by base-token name). If HL doesn't quote
		// the asset (e.g. delisted/edge tokens like HPL), emit the snapshot
		// with no oracle/usd — the dashboard will treat it as 0 USD, which
		// is correct for assets we can't price. Skipping the asset entirely
		// would lose the balance fact; failing the whole account would
		// poison every other account in the cycle.
		px, ok := spotPrices[asset]
		if !ok {
			balances = append(balances, &models.BalanceSnapshot{
				Asset:       asset,
				Balance:     b.Total,
				TimestampMs: nowMs,
			})
			continue
		}
		pxF, err := strconv.ParseFloat(px, 64)
		if err != nil {
			return nil, fmt.Errorf("hyperliquid: failed to parse mark price %q for asset %s: %w", px, asset, err)
		}
		usd := total * pxF
		usdStr := cleanDecimal(strconv.FormatFloat(usd, 'f', -1, 64))
		pxCopy := px
		balances = append(balances, &models.BalanceSnapshot{
			Asset:       asset,
			Balance:     b.Total,
			OraclePrice: &pxCopy,
			UsdValue:    &usdStr,
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

// hlFundingWindow is the length of a single Hyperliquid funding settlement
// window for perps. HL's userFunding.time marks the START of the window
// (hour-aligned). The funding payment is accrued across n_samples windows
// of this length and actually settles at the END of the accrual period.
const hlFundingWindow = time.Hour

// transformFunding converts a Hyperliquid funding entry to a TransferInput.
//
// The HL API's `userFunding.time` field is the START of the funding
// accrual period; the payment actually settles at the END of the period.
// HL's bucket cadence varies — hourly buckets carry n_samples=1 while
// daily-rollup buckets carry n_samples=24 — so we shift the stored
// timestamp by `n_samples * window_length_sec * 1000ms` to land it at
// end-of-accrual. The external ID and metadata window_start_ms are both
// keyed off the RAW `time` so rows stay stable across replays and so
// downstream consumers (R2 detector) can recover the raw start.
//
// If n_samples is missing or zero (very early HL data or malformed
// responses), we fall back to the legacy +1h shift to preserve
// backwards-compatible behavior.
//
// Edge case: positions opened in the final hour of a daily-rollup bucket
// can still have end_of_accrual < first_fill_ts. We accept this rare
// case — the activity_processor sees funding-before-fill and halts, which
// is the correct conservative behavior; a follow-up post-processor could
// nudge funding to the first matching fill if needed.
func transformFunding(entry hlFundingEntry, accountUUID uuid.UUID) *models.TransferInput {
	externalID := fundingExternalID(entry)

	nSamples := entry.Delta.NSamples
	var shift time.Duration
	if nSamples > 0 {
		shift = time.Duration(nSamples) * hlFundingWindow
	} else {
		// Backwards-compat: missing/zero n_samples → assume one hourly bucket.
		shift = hlFundingWindow
	}
	endOfAccrualTs := time.UnixMilli(entry.Time).UTC().Add(shift)

	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeFunding,
		Asset:             "USDC",
		Amount:            cleanDecimal(entry.Delta.Usdc), // Keep signed — Drift also stores signed funding amounts
		Timestamp:         endOfAccrualTs,
		ExternalID:        externalID,
		Metadata: map[string]string{
			"market":            entry.Delta.Coin + "-PERP",
			"funding_rate":      entry.Delta.FundingRate,
			"n_samples":         strconv.Itoa(entry.Delta.NSamples),
			"payment_id":        externalID,
			"window_start_ms":   strconv.FormatInt(entry.Time, 10),
			"window_length_sec": strconv.FormatInt(int64(hlFundingWindow/time.Second), 10),
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
		// HL's rewardsClaim delta places the claim size in `amount` (not `usdc`).
		// Using Delta.Usdc here stores 0 and leaves a residual at reconciliation.
		transferType = models.TypeReward
		asset = "USDC"
		amountStr = entry.Delta.Amount

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
		//
		// IMPORTANT: HL ledger `send` entries report the transfer fee in USDC,
		// not in the token being sent. When the user sends a non-USDC token
		// (e.g. HYPE), folding a USDC fee directly into the HYPE amount
		// produces a phantom HYPE position equal to the fee. To avoid this
		// unit confusion, we only fold the fee when the asset being sent is
		// USDC. For non-USDC sends the USDC fee is emitted separately by
		// extraLedgerFeeTransfer (called from FetchDeposits).
		if !incoming && isUSDCOrEmpty(asset) {
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
// Net interest = supply - borrow. Positive = earned (credit to USDC pile),
// negative = paid (debit from USDC pile). Zero net is skipped.
// Uses math/big.Rat for exact decimal arithmetic.
//
// The SIGN of Amount is load-bearing: Type=interest has no direction field,
// so the processor's ApplyInterest reads the sign of Amount directly
// (positive → credit pile, negative → debit pile / short side). A prior
// version of this function stripped the negative sign before storage with
// the comment "direction is determined by type", but that's wrong for
// TypeInterest — there is no direction field. The result was that every
// event where borrow > supply (interest CHARGED, money lost) got stored
// as a positive USDC credit, inflating USDC piles by 2x the cumulative
// charged interest on every HL account with BLI history (verified on
// b8a094d2: 196 events, +$0.40 stored vs reality -$0.40, $0.80 mismatch
// matching the dashboard's observed gap).
func transformBorrowLendInterest(entry hlBorrowLendInterest, accountUUID uuid.UUID) *models.TransferInput {
	supply := new(big.Rat)
	if _, ok := supply.SetString(entry.Supply); !ok {
		supply.SetInt64(0)
	}

	borrow := new(big.Rat)
	if _, ok := borrow.SetString(entry.Borrow); !ok {
		borrow.SetInt64(0)
	}

	// net = supply - borrow. Signed: positive = earned, negative = paid.
	net := new(big.Rat).Sub(supply, borrow)

	if net.Sign() == 0 {
		return nil
	}

	// Format the net amount as a decimal string, preserving sign.
	amount := net.FloatString(18)
	amount = cleanDecimal(amount)

	// If after cleaning the amount is zero or dust, skip.
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

// isUSDCOrEmpty returns true if asset is empty, "USDC", or any case-variant
// thereof. HL ledger entries that move USDC sometimes report Token="" (the
// USDC field is set instead) and sometimes Token="USDC".
func isUSDCOrEmpty(asset string) bool {
	a := strings.ToUpper(strings.TrimSpace(asset))
	return a == "" || a == "USDC"
}

// extraLedgerFeeTransfer returns a SEPARATE USDC withdraw transfer for ledger
// entries that charge a USDC-denominated fee that can't be folded into the
// main entry's amount field. Two patterns are covered:
//
//  1. `send` / `spotTransfer` of a non-USDC token (e.g. HYPE) with a
//     USDC-denominated fee. Folding the USDC fee into the token amount would
//     produce a phantom position in that token (the original eeb650d7 /
//     0c05e3a5 halt cause). Sender-side only — incoming senders eat the fee
//     on their own ledger.
//
//  2. `internalTransfer` (USDC L1 transfer between HL users): HL charges
//     the recipient ~$1 USDC. The entry appears on both ledgers with
//     fee=1.0, but the debit is the recipient's — not the sender's. Without
//     this row, the recipient's account derives +$1 USDC vs the HL snapshot.
//     Recipient-side only.
//
// Returns nil when:
//   - the entry type is not one of the covered patterns above,
//   - the directional rule for that type doesn't match this wallet,
//   - for sends/spotTransfers, the asset is USDC/empty (the main path's
//     foldLedgerFee already applies — emitting a second row would double-count),
//   - the fee field is empty or zero.
//
// External ID is derived from the source hash plus a "_fee" suffix so the
// fee row does not collide with the main transfer's unique constraint.
func extraLedgerFeeTransfer(entry hlLedgerEntry, accountUUID uuid.UUID, walletAddress string) *models.TransferInput {
	deltaType := strings.ToLower(entry.Delta.Type)
	switch deltaType {
	case "send", "spottransfer":
		// Only outgoing — incoming senders eat the fee on their own ledger.
		if !strings.EqualFold(entry.Delta.User, walletAddress) {
			return nil
		}
		if strings.EqualFold(entry.Delta.User, entry.Delta.Destination) {
			// Self-send — main transformer already returns nil. Don't emit a fee row either.
			return nil
		}
		// Only when the asset moved is non-USDC. USDC sends fold the fee directly.
		if isUSDCOrEmpty(entry.Delta.Token) {
			return nil
		}
	case "internaltransfer":
		// HL charges the $1 internalTransfer fee to the RECIPIENT, not the
		// sender. Empirically verified against accounts eeb650d7 and 42c49379
		// where the only fee=1.0 events on each were internalTransfers in
		// which the account was the recipient, and each carried a +$1 phantom
		// USDC residual vs the HL snapshot.
		//
		// Only emit on the recipient side (incoming). The sender's main
		// withdraw row already equals their actual debit (= usdc, no fee).
		if !strings.EqualFold(entry.Delta.Destination, walletAddress) {
			return nil
		}
		if strings.EqualFold(entry.Delta.User, entry.Delta.Destination) {
			// Self-transfer — no real movement; skip.
			return nil
		}
	default:
		return nil
	}

	feeRat := new(big.Rat)
	if entry.Delta.Fee == "" {
		return nil
	}
	if _, ok := feeRat.SetString(entry.Delta.Fee); !ok {
		return nil
	}
	if feeRat.Sign() == 0 {
		return nil
	}
	absFee := new(big.Rat).Abs(feeRat)
	feeAmount := cleanDecimal(absFee.FloatString(18))

	metadata := map[string]string{
		"payment_id":  entry.Hash,
		"source_type": deltaType + "_fee",
		"fee_token":   "USDC",
	}
	// fee_asset records the asset whose movement triggered the fee. For
	// send/spotTransfer this is the moved token (HYPE etc); for
	// internalTransfer the moved asset is USDC itself.
	if deltaType == "internaltransfer" {
		metadata["fee_asset"] = "USDC"
	} else {
		metadata["fee_asset"] = entry.Delta.Token
	}

	// Place the fee row 1ms after the main transfer so the processor sees
	// the main movement before the fee. Critical for incoming
	// `internalTransfer`: the fee withdraw must NOT execute before the
	// corresponding deposit lands, otherwise FIFO would open a short USDC
	// position for the fee and the subsequent deposit would trip the
	// "exceeds short inventory" guard (eeb650d7 hit this on a fresh
	// account whose first USDC event was a 5-USDC internalTransfer in with
	// a 1-USDC fee). For send/spotTransfer (sender-side outflow) the offset
	// is harmless — fee and main are both withdraws against an established
	// USDC balance.
	return &models.TransferInput{
		ExchangeAccountID: accountUUID,
		Type:              models.TypeWithdraw,
		Asset:             "USDC",
		Amount:            feeAmount,
		Timestamp:         time.UnixMilli(entry.Time + 1).UTC(),
		ExternalID:        entry.Hash + "_fee",
		Metadata:          metadata,
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
