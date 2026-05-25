package lighter

import (
	"context"
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

const (
	defaultBaseURL = "https://mainnet.zklighter.elliot.ai/api/v1"
	httpTimeout    = 15 * time.Second
	maxRetries     = 5
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
	backoffMult    = 2.0
)

// Client implements iface.ExchangeClient for Lighter DEX.
type Client struct {
	baseURL    string
	httpClient *http.Client
	limiter    *rateLimiter // nil means use globalLimiter
	transport  httpcache.Transport
	// principal is the per-account auth scope set at the top of every
	// Fetch* method (api_key when available, else account_index). It is
	// read inside doGet so we don't have to thread the value through
	// every pagination helper. Reset to "" when no Fetch* is in flight.
	principal string
}

// NewClient creates a new Lighter client.
func NewClient() *Client {
	tr, err := httpcache.NewFromEnv()
	if err != nil {
		log.Printf("lighter: httpcache disabled (%v) — falling back to passthrough", err)
		tr = httpcache.Passthrough()
	}
	return &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
		transport:  tr,
	}
}

// Name returns the exchange identifier.
func (c *Client) Name() string {
	return "lighter"
}

// principalFor returns the per-account scope key the httpcache uses to keep
// cached responses from aliasing across accounts. We prefer api_key (stable,
// uniquely scopes auth) and fall back to account_index so an
// unauthenticated lookup still has a non-empty principal.
func principalFor(account *models.ExchangeAccount) string {
	if k := getAPIKey(account); k != "" {
		return k
	}
	if account != nil {
		return account.AccountIdentifier
	}
	return ""
}

// getAPIKey extracts the API key from an exchange account's metadata.
func getAPIKey(account *models.ExchangeAccount) string {
	if account == nil || account.AccountTypeMetadata == nil {
		return ""
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(account.AccountTypeMetadata, &meta); err == nil {
		if key, ok := meta["api_key"].(string); ok {
			return key
		}
	}
	return ""
}

// doGet performs a GET request with rate limiting and retry on 429.
func (c *Client) doGet(ctx context.Context, url string) (json.RawMessage, error) {
	return c.doGetWithAuth(ctx, url, "")
}

// doGetWithAuth performs a GET request with an optional auth token. When a
// cache transport is enabled it intercepts the response; otherwise this is
// the live HTTP path.
func (c *Client) doGetWithAuth(ctx context.Context, url string, authToken string) (json.RawMessage, error) {
	if c.transport != nil && c.transport.IsEnabled() && c.principal != "" {
		body, err := c.transport.Get(ctx, "lighter", url, c.principal, func(ctx context.Context) ([]byte, error) {
			return c.liveGetWithAuth(ctx, url, authToken)
		})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}
	return c.liveGetWithAuth(ctx, url, authToken)
}

// liveGetWithAuth is the original live-HTTP path with rate-limit + retry.
// Kept as a separate function so the cache transport can call it as its
// upstream Fetcher without re-entering the cache.
func (c *Client) liveGetWithAuth(ctx context.Context, url string, authToken string) (json.RawMessage, error) {
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		lim := c.limiter
		if lim == nil {
			lim = globalLimiter
		}
		if err := lim.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if authToken != "" {
			req.Header.Set("Authorization", authToken)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()

			if attempt == maxRetries {
				return nil, &iface.RateLimitError{
					Exchange:   "lighter",
					Message:    fmt.Sprintf("rate limit exceeded after %d retries", maxRetries),
					RetryAfter: backoff,
				}
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			backoff = time.Duration(float64(backoff) * backoffMult)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// 404 is a legitimate "no data" for Lighter history endpoints.
		if resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		// 400 may indicate "account not found" (code 21100) — treat as not found.
		if resp.StatusCode == http.StatusBadRequest {
			var errResp struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(body, &errResp) == nil && errResp.Code == 21100 {
				return nil, nil
			}
			return nil, fmt.Errorf("lighter: 400 bad request | url=%s | body=%s", url, string(body))
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// FetchTrades fetches trades from the Lighter trades API.
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, []*models.PriceRecord, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	c.principal = principalFor(account)
	defer func() { c.principal = "" }()

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account ID: %w", err)
	}

	accountIndex, err := strconv.Atoi(account.AccountIdentifier)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account_index %q: %w", account.AccountIdentifier, err)
	}

	authToken, err := buildAuthToken(getAPIKey(account), accountIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build auth token: %w", err)
	}

	// sinceMs threads the caller's `since` time into pagination so we early-exit
	// once a page tail is older than the cutoff. Without this every sync paged
	// the full account history (45-65 min for 14 accounts); with it, an
	// incremental cycle completes in single-digit minutes. We pass 0 when since
	// is zero so the initial backfill still fetches everything.
	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}

	rawTrades, err := fetchAllTrades(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/trades?account_index=%d&sort_by=timestamp&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, sinceMs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch trades: %w", err)
	}

	// Fetch the liquidation feed alongside trades so we can tag the trades
	// that correspond to forced liquidations. The tag (carried via
	// Trade.TxSignature="lighter-liq:<id>") is purely diagnostic / for
	// classification in downstream tests and reports; it does NOT gate any
	// accounting behavior. Liquidation trades flow through the same uniform
	// fee + FIFO close-PnL path as regular trades — trade.fee captures the
	// taker fee, FIFO close PnL captures the position loss, and together
	// they sum to |deltaCollateral| for each liquidation by algebraic
	// identity (no separate liq_<id>-settlement transfer is emitted).
	//
	// Liquidations pair with trades on exact (market_id, timestamp_ms) match —
	// they are emitted by Lighter at the same millisecond. So using the same
	// sinceMs cutoff here as for trades is safe: a liquidation older than the
	// cutoff has its paired trade also older than the cutoff, and neither would
	// be emitted by this sync.
	rawLiquidations, err := fetchAllLiquidations(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/liquidations?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, sinceMs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch liquidations: %w", err)
	}

	// Build a (market_id, executed_at_ms) -> liquidation_id index. The Lighter
	// API exposes one /trades row per liquidation with identical timestamp_ms
	// AND market_id to the /liquidations row — we use the pair as the join key
	// because the liquidations feed does not carry the underlying trade_id.
	// Collisions (two distinct liquidations in the same market at the same
	// millisecond) are vanishingly rare in practice; if one occurs we keep the
	// first ID we see and log nothing — the tag is purely diagnostic now, so
	// the specific id is irrelevant.
	type liqKey struct {
		marketID  int
		timestamp int64
	}
	liqByTradeKey := make(map[liqKey]int64, len(rawLiquidations))
	for _, rl := range rawLiquidations {
		k := liqKey{marketID: rl.MarketID, timestamp: rl.ExecutedAt}
		if _, exists := liqByTradeKey[k]; !exists {
			liqByTradeKey[k] = rl.ID
		}
	}

	trades := make([]*models.TradeInput, 0, len(rawTrades))
	prices := make([]*models.PriceRecord, 0, len(rawTrades))

	for _, rt := range rawTrades {
		if !since.IsZero() && rt.Timestamp < sinceMs {
			continue
		}

		market, err := c.resolveMarket(ctx, rt.MarketID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve market %d: %w", rt.MarketID, err)
		}

		// Determine side: if account is the bid account, they're buying; if ask, selling.
		side := "buy"
		orderID := rt.BidIDStr
		if rt.AskAccountID == accountIndex {
			side = "sell"
			orderID = rt.AskIDStr
		}

		// Determine the user's role in the trade. is_maker_ask tells us which
		// side filled as maker; the user's role follows from which side they
		// were on. Crash-loud if the user is on neither side — per the
		// no-silent-skip policy, an account-scoped /trades query returning a
		// row the user isn't part of is a data-source regression we want
		// surfaced, not silently miscounted.
		userIsTaker := (rt.IsMakerAsk && rt.BidAccountID == accountIndex) ||
			(!rt.IsMakerAsk && rt.AskAccountID == accountIndex)
		userIsMaker := (rt.IsMakerAsk && rt.AskAccountID == accountIndex) ||
			(!rt.IsMakerAsk && rt.BidAccountID == accountIndex)
		if !userIsTaker && !userIsMaker {
			return nil, nil, fmt.Errorf("lighter trade %d: account %d not on either side (bid=%d, ask=%d)",
				rt.TradeID, accountIndex, rt.BidAccountID, rt.AskAccountID)
		}

		// Lighter's `maker_fee`/`taker_fee` are RATES in micro-percent (e.g.
		// 200 = 2 bps = 0.02%), NOT dollar amounts. Multiply by usd_amount and
		// divide by 1e6 to get the USDC fee. Verified against the Lighter UI's
		// CSV export on 6 samples — exact match to the cent.
		var rateMicroPct int64
		if userIsTaker {
			rateMicroPct = rt.TakerFee // 0 when JSON returns null (no fee)
		} else {
			rateMicroPct = rt.MakerFee
		}
		notional, ok := new(big.Rat).SetString(strings.TrimSpace(rt.UsdAmount))
		if !ok || notional == nil {
			return nil, nil, fmt.Errorf("lighter trade %d: invalid usd_amount %q", rt.TradeID, rt.UsdAmount)
		}
		feeRat := new(big.Rat).Mul(big.NewRat(rateMicroPct, 1), notional)
		feeRat.Quo(feeRat, big.NewRat(1_000_000, 1))

		ts := time.UnixMilli(rt.Timestamp).UTC()

		// Tag liquidation trades. TxSignature is the only persisted field that
		// (a) survives the round-trip through trades-table inserts and (b)
		// isn't already used for Lighter trades (Lighter has no on-chain tx
		// signature). The tag is kept for downstream classification (tests,
		// reports, diagnostics) but is no longer load-bearing for accounting:
		// liquidation trades flow through the same uniform fee + close-PnL
		// path as regular trades. trade.fee captures the taker fee, FIFO
		// close PnL captures the position loss; together they sum to
		// |deltaCollateral| for each liquidation by algebraic identity.
		txSig := ""
		if liqID, ok := liqByTradeKey[liqKey{marketID: rt.MarketID, timestamp: rt.Timestamp}]; ok {
			txSig = fmt.Sprintf("lighter-liq:%d", liqID)
		}

		// Uniform fee computation for all perp trades (liq-tagged included):
		// taker_fee × usd_amount / 1e6 (see feeRat above).
		feeStr := cleanDecimal(feeRat.FloatString(6))

		trades = append(trades, &models.TradeInput{
			TradeID:           rt.TradeIDStr,
			OrderID:           orderID,
			BaseAsset:         market.BaseAsset,
			QuoteAsset:        market.QuoteAsset,
			Side:              side,
			Price:             cleanDecimal(rt.Price),
			Quantity:          cleanDecimal(rt.Size),
			Fee:               feeStr,
			Timestamp:         ts,
			ExchangeAccountID: accountUUID,
			MarketType:        market.MarketType,
			FeeAsset:          "USDC",
			TxSignature:       txSig,
		})

		if rt.Price != "" && rt.Price != "0" {
			prices = append(prices, &models.PriceRecord{
				Asset:        market.BaseAsset,
				Denomination: market.QuoteAsset,
				Timestamp:    ts,
				Price:        cleanDecimal(rt.Price),
				Source:       "execution",
			})
		}
	}

	// Sort ascending by timestamp
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].Timestamp.Before(prices[j].Timestamp)
	})

	return trades, prices, nil
}

// FetchFundingPayments fetches funding payments from the Lighter positionFunding API.
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.principal = principalFor(account)
	defer func() { c.principal = "" }()

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	accountIndex, err := strconv.Atoi(account.AccountIdentifier)
	if err != nil {
		return nil, fmt.Errorf("invalid account_index %q: %w", account.AccountIdentifier, err)
	}

	authToken, err := buildAuthToken(getAPIKey(account), accountIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to build auth token: %w", err)
	}

	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}

	rawFunding, err := fetchAllFunding(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/positionFunding?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch funding: %w", err)
	}

	payments := make([]*models.TransferInput, 0, len(rawFunding))

	for _, rf := range rawFunding {
		// Lighter funding timestamps are Unix seconds. Compare against the
		// original `since` (which may have sub-second precision from the +1ms
		// increment in account_syncer). Using time.Time comparison ensures a
		// funding payment at second X is correctly skipped when since is X+1ms.
		if !since.IsZero() && !time.Unix(rf.Timestamp, 0).After(since) {
			continue
		}

		market, err := c.resolveMarket(ctx, rf.MarketID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve market %d: %w", rf.MarketID, err)
		}

		fundingIDStr := strconv.FormatInt(rf.FundingID, 10)

		payments = append(payments, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeFunding,
			Asset:             market.QuoteAsset,
			Amount:            cleanDecimal(rf.Change),
			Timestamp:         time.Unix(rf.Timestamp, 0).UTC(),
			ExternalID:        fundingIDStr,
			Metadata: map[string]string{
				"market":     market.Symbol + "-PERP",
				"payment_id": fundingIDStr,
			},
		})
	}

	// Sort ascending by timestamp
	sort.Slice(payments, func(i, j int) bool {
		return payments[i].Timestamp.Before(payments[j].Timestamp)
	})

	return payments, nil
}

// FetchDeposits fetches deposits and withdrawals from the Lighter deposit history API.
func (c *Client) FetchDeposits(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, []*models.PriceRecord, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	c.principal = principalFor(account)
	defer func() { c.principal = "" }()

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account ID: %w", err)
	}

	accountIndex, err := strconv.Atoi(account.AccountIdentifier)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid account_index %q: %w", account.AccountIdentifier, err)
	}

	authToken, err := buildAuthToken(getAPIKey(account), accountIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build auth token: %w", err)
	}

	// Extract l1_address from account metadata or use wallet address
	l1Address := extractL1Address(account)

	// sinceMs early-exits pagination once a page tail is older than the cutoff.
	// For withdraws and transfers we widen the cutoff by the L1↔L2 fast-withdraw
	// pairing window (10 min late-L2 + 1 min buffer = 11 min) so that an L1 near
	// the boundary still has its asymmetric-window L2 partner reachable in the
	// same sync pass. Without the widening, a since-bounded fetch could drop the
	// L2 (or L1) side of a pair and re-emit the surviving leg, double-debiting USDC.
	var sinceMs, pairSinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
		pairSinceMs = sinceMs - 660000 // 11 min (10min L2-lag window + 1min buffer)
		if pairSinceMs < 0 {
			pairSinceMs = 0
		}
	}

	rawDeposits, err := fetchAllDeposits(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/deposit/history?account_index=%d&l1_address=%s&filter=all&limit=%d",
			c.baseURL, accountIndex, l1Address, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, sinceMs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch deposits: %w", err)
	}

	// Fetch L2 internal transfers (spot<->perp, cross-account)
	rawTransfers, err := fetchAllTransfers(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/transfer/history?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, pairSinceMs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch transfers: %w", err)
	}

	// Fetch L1 withdrawals (not captured by deposit/history)
	rawWithdraws, err := fetchAllWithdraws(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/withdraw/history?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken, pairSinceMs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch withdrawals: %w", err)
	}

	// Note: liquidation events are NOT fetched here. Liquidations now flow
	// through the uniform perp-trade accounting path — the embedded /trades
	// row carries the taker fee on trade.fee (debits Balance), and the
	// processor's FIFO close-PnL credits the spot:USDC settlement (capturing
	// the position loss). Together these sum to |deltaCollateral| for each
	// liquidation by algebraic identity (the processor uses FIFO cost basis;
	// Lighter's deltaCollateral uses weighted-average cost basis, but for a
	// fully-closed position the totals match across cost-basis methods).
	// Tagging the trade row with TxSignature="lighter-liq:<id>" still happens
	// in FetchTrades (for downstream classification), but no separate
	// liq_<id>-settlement TypeFee transfer is emitted from FetchDeposits.

	transfers := make([]*models.TransferInput, 0, len(rawDeposits)+len(rawTransfers)+len(rawWithdraws))

	// Lighter exposes a fast-withdraw twice when the user pre-funds via L2SelfTransfer
	// (intra-account spot↔perps): once via /withdraw/history (L1 settlement) and once
	// via /transfer/history (L2 outflow to to_account_index=3, the bridge account).
	// We dedup by dropping the L1 leg when a paired L2 outflow exists.
	//
	// EMPIRICAL CASE (verified May 2026): when the user pre-funds via L2TransferInflow
	// from another Lighter account (not via self-transfer), Lighter settles the
	// fast-withdraw L1-side only — NO L2 outflow row is ever emitted. Examples:
	// fast-531694 (acct 285860), fast-556070 / fast-564928 (acct 711452). For these,
	// after a 15-min confirmation window with no L2 partner found, we emit the L1
	// row directly + a synthetic bridge_fee from /transferFeeInfo, and log a loud
	// WARN tagged "api-orphan" for audit. This is NOT a soft-default fallback for
	// unknown failures — it's an explicit handling of a known Lighter behavior.
	//
	// Matching strategy: for each completed L1 fast-withdraw rw (sorted ascending
	// by timestamp), find a matching L2 record (sorted ascending by L2.id) using
	// greedy first-fit consumption:
	//
	//   A) L2TransferOutflow with to_account_index == 3 (Lighter's bridge).
	//      The L2 record IS the canonical outflow; drop the L1 leg. The L2
	//      loop emits the withdraw and (if Fee>0) the USDC fee transfer.
	//
	//   B) L2SelfTransfer with to_account_index == accountIndex (intra-account
	//      prep). L2SelfTransfer is skipped by the L2 loop, so the L1
	//      fast-withdraw IS the canonical outflow. If the pre-staged GROSS
	//      amount exceeded the L1 net, emit the gap as a synthetic
	//      bridge_fee_<id> USDC transfer (fee_rule="self-transfer-bridge").
	//
	// Pairing requires:
	//   - L2.timestamp ∈ [L1.timestamp - 60s, L1.timestamp + 600s]  ASYMMETRIC
	//     window, inclusive both ends. Empirically Lighter posts the bridge
	//     L2 outflow up to +226s AFTER the L1 fast-withdraw (verified case
	//     80ecf79e); the 10-min ceiling absorbs that plus headroom. The -60s
	//     side preserves the self-transfer prep case where L2 precedes L1.
	//   - asset_id matches,
	//   - to_account_index ∈ {3, accountIndex},
	//   - L2.amount == L1.amount (LIT-staked fee waiver) OR
	//     L2.amount - L1.amount == current_bridge_fee_usdc (fetched from
	//     /transferFeeInfo, NOT hardcoded), within $0.01.
	//
	// Each L2 record is consumed at most once (tracked via consumedL2Ids).
	//
	// If a completed fast-withdraw is older than 15 minutes with no L2 partner,
	// it's treated as an api-orphan (L2TransferInflow pre-fund path): emit the
	// L1 row directly plus a synthetic bridge_fee (fee_rule="api-orphan") and
	// log a loud WARN. If the unmatched L1 is younger than 15 min, we defer
	// (don't emit) and let the next cycle re-attempt.
	type pairKind int
	const (
		pairBridge pairKind = iota // L2TransferOutflow to bridge (to_account_index=3)
		pairSelf                   // L2SelfTransfer (to_account_index=accountIndex)
	)
	type bridgeFeeEmit struct {
		rw   lighterWithdraw
		fee  *big.Rat
		rule string // "self-transfer-bridge" or "api-orphan"
	}
	pairedL1Withdraws := make(map[string]bool, len(rawWithdraws))
	deferredL1Withdraws := make(map[string]bool, len(rawWithdraws))
	consumedL2Ids := make(map[string]bool, len(rawTransfers))
	var bridgeFeeEmits []bridgeFeeEmit

	// Cache the current bridge fee from /transferFeeInfo. Fetched lazily on
	// the first L1 fast-withdraw that needs it and reused for the rest of the
	// FetchDeposits call.
	var cachedBridgeFeeUSDC *big.Rat
	var cachedBridgeFeeErr error
	var cachedBridgeFeeFetched bool
	getBridgeFeeUSDC := func() (*big.Rat, error) {
		if cachedBridgeFeeFetched {
			return cachedBridgeFeeUSDC, cachedBridgeFeeErr
		}
		cachedBridgeFeeFetched = true
		cachedBridgeFeeUSDC, cachedBridgeFeeErr = fetchCurrentBridgeFeeUSDC(ctx, c, accountIndex, authToken)
		return cachedBridgeFeeUSDC, cachedBridgeFeeErr
	}

	if len(rawWithdraws) > 0 {
		const (
			pairWindowEarlyMs = int64(60000)  // 60 seconds before L1 (self-transfer prep)
			pairWindowLateMs  = int64(600000) // 10 minutes after L1 (bridge L2 lag)
			ageThresholdMs    = int64(900000) // 15 min (10 min late window + 5 min safety)
		)
		eps := big.NewRat(1, 100) // $0.01 tolerance
		accountIndex64 := int64(accountIndex)

		// Sort L1 ascending by timestamp; sort L2 ascending by numeric id for
		// deterministic greedy consumption on burst pairs with identical
		// amount/timestamp.
		sortedL1 := make([]lighterWithdraw, len(rawWithdraws))
		copy(sortedL1, rawWithdraws)
		sort.Slice(sortedL1, func(i, j int) bool {
			return sortedL1[i].Timestamp < sortedL1[j].Timestamp
		})
		sortedL2 := make([]lighterTransfer, len(rawTransfers))
		copy(sortedL2, rawTransfers)
		sort.Slice(sortedL2, func(i, j int) bool {
			ai, errA := strconv.ParseInt(sortedL2[i].ID, 10, 64)
			bj, errB := strconv.ParseInt(sortedL2[j].ID, 10, 64)
			if errA == nil && errB == nil {
				return ai < bj
			}
			return sortedL2[i].ID < sortedL2[j].ID
		})

		nowMs := time.Now().UnixMilli()

		for _, rw := range sortedL1 {
			if rw.Type != "fast" {
				continue
			}
			switch rw.Status {
			case "completed":
				// fall through to matcher
			case "pending", "claimable":
				deferredL1Withdraws[rw.ID] = true
				continue
			case "refunded", "failed":
				deferredL1Withdraws[rw.ID] = true
				continue
			default:
				return nil, nil, fmt.Errorf("lighter: fast-withdraw %s has unknown status %q (account=%d, l1_tx_hash=%s)",
					rw.ID, rw.Status, accountIndex, rw.L1TxHash)
			}

			rwAmt, ok := new(big.Rat).SetString(strings.TrimPrefix(rw.Amount, "-"))
			if !ok || rwAmt == nil {
				return nil, nil, fmt.Errorf("lighter: fast-withdraw %s has unparseable amount %q (account=%d)", rw.ID, rw.Amount, accountIndex)
			}

			matched := false
			var matchedKind pairKind
			var matchedFee *big.Rat
			var matchedL2ID string

			for _, rt := range sortedL2 {
				if consumedL2Ids[rt.ID] {
					continue
				}
				if rt.AssetID != rw.AssetID {
					continue
				}
				var kind pairKind
				switch rt.Type {
				case "L2TransferOutflow":
					if rt.ToAccountIndex != 3 {
						continue
					}
					kind = pairBridge
				case "L2SelfTransfer":
					if rt.ToAccountIndex != accountIndex64 {
						continue
					}
					kind = pairSelf
				default:
					continue
				}
				dt := rt.Timestamp - rw.Timestamp
				if dt < -pairWindowEarlyMs || dt > pairWindowLateMs {
					continue
				}
				rtAmt, ok := new(big.Rat).SetString(strings.TrimPrefix(rt.Amount, "-"))
				if !ok || rtAmt == nil {
					continue
				}
				diff := new(big.Rat).Sub(rtAmt, rwAmt) // L2 - L1
				absDiff := new(big.Rat).Abs(new(big.Rat).Set(diff))
				var fee *big.Rat
				if absDiff.Cmp(eps) <= 0 {
					fee = new(big.Rat)
				} else {
					feeUSDC, feeErr := getBridgeFeeUSDC()
					if feeErr != nil {
						continue
					}
					feeGap := new(big.Rat).Sub(diff, feeUSDC)
					if new(big.Rat).Abs(feeGap).Cmp(eps) <= 0 {
						fee = new(big.Rat).Set(feeUSDC)
					} else {
						continue
					}
				}
				matched = true
				matchedKind = kind
				matchedFee = fee
				matchedL2ID = rt.ID
				break
			}

			if matched {
				consumedL2Ids[matchedL2ID] = true
				switch matchedKind {
				case pairBridge:
					pairedL1Withdraws[rw.ID] = true
				case pairSelf:
					if matchedFee != nil && matchedFee.Sign() > 0 {
						bridgeFeeEmits = append(bridgeFeeEmits, bridgeFeeEmit{
							rw:   rw,
							fee:  matchedFee,
							rule: "self-transfer-bridge",
						})
					}
				}
				continue
			}

			ageMs := nowMs - rw.Timestamp
			if ageMs >= ageThresholdMs {
				// api-orphan: Lighter pre-funded via L2TransferInflow (not
				// self-transfer) so no L2 outflow row exists. Emit the L1 leg
				// directly + a synthetic bridge_fee, log loud WARN for audit.
				feeUSDC, feeErr := getBridgeFeeUSDC()
				if feeErr != nil {
					return nil, nil, fmt.Errorf("lighter: api-orphan fast-withdraw %s: failed to fetch bridge fee: %w | account_index=%d | l1_tx_hash=%s",
						rw.ID, feeErr, accountIndex, rw.L1TxHash)
				}
				log.Printf("WARN lighter: api-orphan fast-withdraw %s has no L2 partner after %dms; emitting L1 directly with synthetic bridge_fee=%s | account_index=%d | l1_tx_hash=%s | amount=%s | timestamp=%d | age_ms=%d",
					rw.ID, ageMs, feeUSDC.FloatString(8), accountIndex, rw.L1TxHash, rw.Amount, rw.Timestamp, ageMs)
				if feeUSDC.Sign() > 0 {
					bridgeFeeEmits = append(bridgeFeeEmits, bridgeFeeEmit{
						rw:   rw,
						fee:  new(big.Rat).Set(feeUSDC),
						rule: "api-orphan",
					})
				}
				continue
			}
			log.Printf("WARN lighter: fast-withdraw %s amount=%s timestamp=%d age_ms=%d has no L2 partner yet; deferring to next sync cycle (account=%d)",
				rw.ID, rw.Amount, rw.Timestamp, ageMs, accountIndex)
			deferredL1Withdraws[rw.ID] = true
		}
	}

	// Emit USDC bridge-fee transfers for self-transfer-prep paired withdraws.
	// Lighter's L1 bridge fee is denominated in USDC regardless of the asset
	// being withdrawn. TypeFee is treated as TypeWithdraw by the processor.
	//
	// Gate on sinceMs to mirror the kept-L1 emit at the bottom of this
	// function: if the underlying L1 fast-withdraw was already emitted on a
	// prior sync cycle (its timestamp is older than `since`), its companion
	// bridge_fee_* row was inserted then too. Re-emitting on every incremental
	// sync causes idx_transfers_external_id_unique violations because the
	// matcher's widened pairSinceMs window keeps surfacing the same paired
	// L1 fast-withdraws each cycle.
	for _, entry := range bridgeFeeEmits {
		if !since.IsZero() && entry.rw.Timestamp < sinceMs {
			continue
		}
		rule := entry.rule
		if rule == "" {
			rule = "self-transfer-bridge"
		}
		transfers = append(transfers, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeFee,
			Asset:             "USDC",
			Amount:            cleanDecimal(entry.fee.FloatString(8)),
			Timestamp:         time.UnixMilli(entry.rw.Timestamp).UTC(),
			ExternalID:        fmt.Sprintf("bridge_fee_%s", entry.rw.ID),
			Metadata: map[string]string{
				"withdraw_id": entry.rw.ID,
				"l1_tx_hash":  entry.rw.L1TxHash,
				"fee_rule":    rule,
			},
		})
	}

	for _, rd := range rawDeposits {
		if !since.IsZero() && rd.Timestamp < sinceMs {
			continue
		}

		// Resolve asset symbol from asset_id using spot order book data
		assetSymbol, err := c.resolveAsset(ctx, rd.AssetID)
		if err != nil {
			return nil, nil, fmt.Errorf("lighter: unsupported asset_id %d in deposit %s: %w", rd.AssetID, rd.DepositID, err)
		}

		// Determine deposit vs withdrawal from amount sign
		amount := rd.Amount
		transferType := models.TypeDeposit
		if strings.HasPrefix(amount, "-") {
			transferType = models.TypeWithdraw
			amount = amount[1:] // Make positive
		}

		transfers = append(transfers, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              transferType,
			Asset:             assetSymbol,
			Amount:            cleanDecimal(amount),
			Timestamp:         time.UnixMilli(rd.Timestamp).UTC(),
			ExternalID:        rd.DepositID,
			Metadata: map[string]string{
				"deposit_id": rd.DepositID,
				"tx_hash":    rd.TxHash,
			},
		})
	}

	// Process L2 internal transfers
	for _, rt := range rawTransfers {
		if !since.IsZero() && rt.Timestamp < sinceMs {
			continue
		}

		// Skip self-transfers (spot<->perp within same account)
		if rt.Type == "L2SelfTransfer" {
			continue
		}

		assetSymbol, err := c.resolveAsset(ctx, rt.AssetID)
		if err != nil {
			return nil, nil, fmt.Errorf("lighter: unsupported asset_id %d in transfer %s: %w", rt.AssetID, rt.ID, err)
		}

		var transferType string
		switch rt.Type {
		case "L2TransferInflow", "L2UnstakeAssetInflow":
			transferType = models.TypeDeposit
		case "L2TransferOutflow", "L2StakeAssetOutflow":
			transferType = models.TypeWithdraw
		default:
			// Crash-loud: silent skip here hides accounting bugs (Lighter
			// can introduce new transfer types like L2StakeAssetInflow,
			// insurance-fund debits, vault distributions, etc.; dropping
			// them creates phantom positions that look like processor
			// bugs but are actually missing inputs). Add an explicit case
			// when this fires.
			return nil, nil, fmt.Errorf("lighter: unknown L2Transfer type %q | transfer_id=%s | asset_id=%d | amount=%s | timestamp=%d | tx_hash=%s",
				rt.Type, rt.ID, rt.AssetID, rt.Amount, rt.Timestamp, rt.TxHash)
		}

		amount := rt.Amount
		if strings.HasPrefix(amount, "-") {
			amount = amount[1:]
		}

		transfers = append(transfers, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              transferType,
			Asset:             assetSymbol,
			Amount:            cleanDecimal(amount),
			Timestamp:         time.UnixMilli(rt.Timestamp).UTC(),
			ExternalID:        fmt.Sprintf("transfer_%s", rt.ID),
			Metadata: map[string]string{
				"transfer_id":  rt.ID,
				"tx_hash":      rt.TxHash,
				"transfer_type": rt.Type,
			},
		})

		// Lighter's `rt.Fee` field on L2Transfer records is ALWAYS denominated
		// in USDC (the flat bridge fee, typically $3, occasionally higher for
		// large amounts), regardless of which asset is being transferred. If
		// we attribute this fee to `assetSymbol` for a non-USDC transfer
		// (e.g. LIT), we create a phantom short in that asset AND miss the
		// real USDC outflow. Pin Asset to "USDC" for fee transfers.
		feeStr := strings.TrimPrefix(rt.Fee, "-")
		if cleaned := cleanDecimal(feeStr); cleaned != "0" && cleaned != "" {
			transfers = append(transfers, &models.TransferInput{
				ExchangeAccountID: accountUUID,
				Type:              models.TypeFee,
				Asset:             "USDC",
				Amount:            cleaned,
				Timestamp:         time.UnixMilli(rt.Timestamp).UTC(),
				ExternalID:        fmt.Sprintf("transfer_%s-fee", rt.ID),
				Metadata: map[string]string{
					"transfer_id":   rt.ID,
					"tx_hash":       rt.TxHash,
					"transfer_type": rt.Type,
				},
			})
		}
	}

	// Process L1 withdrawals
	for _, rw := range rawWithdraws {
		if !since.IsZero() && rw.Timestamp < sinceMs {
			continue
		}

		// Skip L1 withdraws that have a paired L2TransferOutflow to the bridge
		// (see pairing logic above). The L2 leg is the canonical record.
		if pairedL1Withdraws[rw.ID] {
			continue
		}

		// Skip fast-withdraws that the matcher decided to defer (pending /
		// refunded / failed status, or completed-but-not-yet-paired and within
		// the 15-min grace window). They'll resurface on the next sync cycle.
		if deferredL1Withdraws[rw.ID] {
			continue
		}

		assetSymbol, err := c.resolveAsset(ctx, rw.AssetID)
		if err != nil {
			return nil, nil, fmt.Errorf("lighter: unsupported asset_id %d in withdrawal %s: %w", rw.AssetID, rw.ID, err)
		}

		amount := rw.Amount
		if strings.HasPrefix(amount, "-") {
			amount = amount[1:]
		}

		transfers = append(transfers, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeWithdraw,
			Asset:             assetSymbol,
			Amount:            cleanDecimal(amount),
			Timestamp:         time.UnixMilli(rw.Timestamp).UTC(),
			ExternalID:        fmt.Sprintf("withdraw_%s", rw.ID),
			Metadata: map[string]string{
				"withdraw_id": rw.ID,
				"l1_tx_hash":  rw.L1TxHash,
			},
		})
	}

	// Sort ascending by timestamp
	sort.Slice(transfers, func(i, j int) bool {
		return transfers[i].Timestamp.Before(transfers[j].Timestamp)
	})

	return transfers, nil, nil
}

// transferFeeInfoResp is the response shape from GET /api/v1/transferFeeInfo.
// `transfer_fee_usdc` is the current flat fast-withdraw bridge fee in
// micro-USDC (1e-6 USDC). We tolerate the field arriving as either a JSON
// number or string by using json.Number.
type transferFeeInfoResp struct {
	Code             int         `json:"code"`
	Message          string      `json:"message"`
	TransferFeeUSDC  json.Number `json:"transfer_fee_usdc"`
}

// fetchCurrentBridgeFeeUSDC fetches the current Lighter fast-withdraw bridge
// fee from /api/v1/transferFeeInfo and returns it as a USDC big.Rat (i.e.
// transfer_fee_usdc / 1_000_000). Used as the soft-default value when an L1
// fast-withdraw has no matching L2 prep record. Called once per FetchDeposits
// call (cached) to avoid extra round-trips.
func fetchCurrentBridgeFeeUSDC(ctx context.Context, c *Client, accountIndex int, authToken string) (*big.Rat, error) {
	url := fmt.Sprintf("%s/transferFeeInfo?account_index=%d", c.baseURL, accountIndex)
	body, err := c.doGetWithAuth(ctx, url, authToken)
	if err != nil {
		return nil, fmt.Errorf("transferFeeInfo request failed: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("transferFeeInfo returned empty body")
	}
	var resp transferFeeInfoResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("transferFeeInfo decode failed: %w", err)
	}
	if resp.TransferFeeUSDC == "" {
		return nil, fmt.Errorf("transferFeeInfo: missing transfer_fee_usdc field (code=%d msg=%q)", resp.Code, resp.Message)
	}
	micro, err := resp.TransferFeeUSDC.Int64()
	if err != nil {
		return nil, fmt.Errorf("transferFeeInfo: invalid transfer_fee_usdc %q: %w", resp.TransferFeeUSDC.String(), err)
	}
	if micro < 0 {
		return nil, fmt.Errorf("transferFeeInfo: negative transfer_fee_usdc %d", micro)
	}
	return new(big.Rat).SetFrac(big.NewInt(micro), big.NewInt(1_000_000)), nil
}

// FetchBalances fetches current balances for a Lighter account.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.principal = principalFor(account)
	defer func() { c.principal = "" }()

	l1Address := extractL1Address(account)
	if l1Address == "" {
		return nil, fmt.Errorf("l1_address is required for balance fetching")
	}

	accountIndex, err := strconv.Atoi(account.AccountIdentifier)
	if err != nil {
		return nil, fmt.Errorf("invalid account_index %q: %w", account.AccountIdentifier, err)
	}

	url := fmt.Sprintf("%s/account?by=l1_address&value=%s", c.baseURL, l1Address)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account: %w", err)
	}

	if body == nil {
		return nil, nil
	}

	var resp lighterAccountResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode account response: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	var balances []*models.BalanceSnapshot

	// zeroUSDC builds the canonical "account exists but holds nothing" row.
	// Lighter accounts (both main and sub) settle all collateral in USDC at a
	// fixed 1:1 price, so an empty account is unambiguously $0 of USDC rather
	// than an unknown asset mix. Emitting this row — instead of returning nil
	// — guarantees every synced Lighter account has at least one
	// spot_balance_snapshots entry, which lets the dashboard distinguish
	// "account empty" from "account never synced".
	zeroUSDC := func() *models.BalanceSnapshot {
		one := "1"
		zero := "0"
		return &models.BalanceSnapshot{
			Asset:       "USDC",
			Balance:     "0",
			OraclePrice: &one,
			UsdValue:    &zero,
			TimestampMs: nowMs,
		}
	}

	for _, acct := range resp.Accounts {
		if acct.AccountIndex != accountIndex {
			continue
		}

		// Sub-accounts (account_type=1) are cross-margin perp accounts: the
		// per-asset Balance fields are always 0 because the collateral is
		// pooled at the account level. The canonical USDC balance for these
		// accounts lives in `collateral`. Without this branch, FetchBalances
		// returns an empty slice for any sub-account that has never held a
		// non-margin spot asset, and no spot_balance_snapshots row is ever
		// written.
		if acct.AccountType == 1 {
			if acct.Collateral == "" {
				// API returned the account but with no collateral field at
				// all (brand-new sub-account that has never been funded).
				// Still emit a zero USDC row so the syncer writes a snapshot.
				return []*models.BalanceSnapshot{zeroUSDC()}, nil
			}
			collateral, err := strconv.ParseFloat(acct.Collateral, 64)
			if err != nil {
				return nil, fmt.Errorf("lighter: failed to parse collateral %q (account_index=%d): %w", acct.Collateral, accountIndex, err)
			}
			if math.Abs(collateral) < 0.000001 {
				// Zero collateral — sub-account exists but holds nothing.
				// Emit one explicit USDC=0 row instead of returning nil; the
				// syncer's empty-balances fallback only writes rows for
				// previously-seen assets, so a brand-new empty sub-account
				// would otherwise have zero spot_balance_snapshots forever.
				return []*models.BalanceSnapshot{zeroUSDC()}, nil
			}
			// Lighter cross-margin sub-accounts hold collateral exclusively
			// in USDC; price is fixed at 1.0 and usd_value mirrors the
			// balance. Setting both eliminates spurious NULLs in
			// spot_balance_snapshots.usd_value that would otherwise inflate
			// dashboard reconciliation gaps.
			one := "1"
			usd := acct.Collateral
			balances = append(balances, &models.BalanceSnapshot{
				Asset:       "USDC",
				Balance:     acct.Collateral,
				OraclePrice: &one,
				UsdValue:    &usd,
				TimestampMs: nowMs,
			})
			return balances, nil
		}

		// Main account (account_type=0): use per-asset balances for non-USDC
		// assets, but source USDC from `collateral` (the canonical equity
		// field). When USDC has margin_mode=enabled, Assets[].Balance is 0
		// and the real value lives in Collateral / Assets[].MarginBalance —
		// mirroring how the account_type=1 branch above already handles it.
		for _, a := range acct.Assets {
			if a.Symbol == "USDC" {
				// Sourced below from acct.Collateral.
				continue
			}
			bal, err := strconv.ParseFloat(a.Balance, 64)
			if err != nil {
				return nil, fmt.Errorf("lighter: failed to parse balance %q for asset %s (account_index=%d): %w", a.Balance, a.Symbol, accountIndex, err)
			}
			if math.Abs(bal) < 0.000001 {
				continue
			}
			// Non-USDC Lighter spot assets aren't priced via /api/v1/account
			// (mark_prices live on /orderBookDetails or /candlesticks); leave
			// price/usd_value nil here so the snapshot syncer treats them as
			// genuinely-unknown rather than zeroed-out, and follow up with a
			// dedicated pricing pass when those assets become material.
			balances = append(balances, &models.BalanceSnapshot{
				Asset:       a.Symbol,
				Balance:     a.Balance,
				TimestampMs: nowMs,
			})
		}
		// USDC: canonical equity comes from top-level `collateral`. This is a
		// 1:1 stablecoin so price=1 and usd_value mirrors the balance.
		if acct.Collateral != "" {
			collateral, err := strconv.ParseFloat(acct.Collateral, 64)
			if err != nil {
				return nil, fmt.Errorf("lighter: failed to parse collateral %q (account_index=%d): %w", acct.Collateral, accountIndex, err)
			}
			if math.Abs(collateral) >= 0.000001 {
				one := "1"
				usd := acct.Collateral
				balances = append(balances, &models.BalanceSnapshot{
					Asset:       "USDC",
					Balance:     acct.Collateral,
					OraclePrice: &one,
					UsdValue:    &usd,
					TimestampMs: nowMs,
				})
			}
		}

		// Main account had no non-zero non-USDC assets and zero/missing
		// collateral. Same reasoning as the sub-account branch: still emit a
		// single USDC=0 row so the account has at least one snapshot.
		if len(balances) == 0 {
			balances = append(balances, zeroUSDC())
		}
		return balances, nil
	}

	// No account in the API response matched the requested account_index.
	// That's a real "account not present on the exchange" condition — leave
	// the existing nil contract so the syncer treats it as a fetch miss
	// rather than synthesizing a fake $0 balance.
	return nil, nil
}

// FetchHistoricalBalanceSnapshots returns nil for Lighter (not supported).
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	return nil, nil
}

// FetchSettlements returns nil for Lighter.
// Lighter settles on close (PnL is credited to balance immediately on position close).
func (c *Client) FetchSettlements(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.Settlement, error) {
	return nil, nil
}

// extractL1Address extracts the L1 (Ethereum) address from account metadata.
func extractL1Address(account *models.ExchangeAccount) string {
	if account.AccountTypeMetadata != nil {
		var meta map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &meta); err == nil {
			if addr, ok := meta["l1_address"].(string); ok && addr != "" {
				return addr
			}
		}
	}
	return ""
}

// microToDecimal converts a micro-USDC integer (1e-6 USDC) to a clean decimal string.
func microToDecimal(v int64) string {
	if v == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d.%06d", v/1000000, v%1000000)
	if v < 0 {
		// Handle negative: fmt already puts the sign on the integer part
		abs := v
		if abs < 0 {
			abs = -abs
		}
		s = fmt.Sprintf("-%d.%06d", abs/1000000, abs%1000000)
	}
	return cleanDecimal(s)
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
