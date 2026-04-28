package lighter

import (
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
}

// NewClient creates a new Lighter client.
func NewClient() *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// Name returns the exchange identifier.
func (c *Client) Name() string {
	return "lighter"
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

// doGetWithAuth performs a GET request with an optional auth token.
func (c *Client) doGetWithAuth(ctx context.Context, url string, authToken string) (json.RawMessage, error) {
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

	rawTrades, err := fetchAllTrades(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/trades?account_index=%d&sort_by=timestamp&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch trades: %w", err)
	}

	sinceMs := since.UnixMilli()
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
		// Determine fee: the API field is_maker_ask tells us which side was maker.
		// If is_maker_ask=true, ask is maker, bid is taker. If false, bid is maker, ask is taker.
		side := "buy"
		orderID := rt.BidIDStr
		var fee int64
		if rt.AskAccountID == accountIndex {
			side = "sell"
			orderID = rt.AskIDStr
			// Ask is our account: if is_maker_ask, we're maker; otherwise taker
			if rt.IsMakerAsk {
				fee = rt.MakerFee
			} else {
				fee = rt.TakerFee
			}
		} else {
			// Bid is our account: if is_maker_ask, we're taker; otherwise maker
			if rt.IsMakerAsk {
				fee = rt.TakerFee
			} else {
				fee = rt.MakerFee
			}
		}

		ts := time.UnixMilli(rt.Timestamp).UTC()

		trades = append(trades, &models.TradeInput{
			TradeID:           rt.TradeIDStr,
			OrderID:           orderID,
			BaseAsset:         market.BaseAsset,
			QuoteAsset:        market.QuoteAsset,
			Side:              side,
			Price:             cleanDecimal(rt.Price),
			Quantity:          cleanDecimal(rt.Size),
			Fee:               microToDecimal(fee),
			Timestamp:         ts,
			ExchangeAccountID: accountUUID,
			MarketType:        market.MarketType,
			FeeAsset:          "USDC",
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

	rawFunding, err := fetchAllFunding(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/positionFunding?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken)
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

	rawDeposits, err := fetchAllDeposits(ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/deposit/history?account_index=%d&l1_address=%s&filter=all&limit=%d",
			c.baseURL, accountIndex, l1Address, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	}, authToken)
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
	}, authToken)
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
	}, authToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch withdrawals: %w", err)
	}

	sinceMs := since.UnixMilli()
	transfers := make([]*models.TransferInput, 0, len(rawDeposits)+len(rawTransfers)+len(rawWithdraws))

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
		case "L2TransferInflow":
			transferType = models.TypeDeposit
		case "L2TransferOutflow", "L2StakeAssetOutflow":
			transferType = models.TypeWithdraw
		default:
			continue // Unknown type, skip
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
	}

	// Process L1 withdrawals
	for _, rw := range rawWithdraws {
		if !since.IsZero() && rw.Timestamp < sinceMs {
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

// FetchBalances fetches current balances for a Lighter account.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

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
				return nil, nil
			}
			collateral, err := strconv.ParseFloat(acct.Collateral, 64)
			if err != nil {
				return nil, fmt.Errorf("lighter: failed to parse collateral %q (account_index=%d): %w", acct.Collateral, accountIndex, err)
			}
			// Emit USDC collateral row tagged wallet_type=perp. Even with zero
			// collateral, we still iterate positions below so a previously-open
			// position that just closed gets a zero row written by the syncer.
			if math.Abs(collateral) >= 0.000001 {
				balances = append(balances, &models.BalanceSnapshot{
					Asset:       "USDC",
					Balance:     acct.Collateral,
					TimestampMs: nowMs,
					WalletType:  "perp",
				})
			}

			// Emit one row per open perp position. `position` is signed
			// (positive=long, negative=short). Skip zero-size to avoid noise.
			for _, p := range acct.Positions {
				if p.Position == "" {
					continue
				}
				size, err := strconv.ParseFloat(p.Position, 64)
				if err != nil {
					return nil, fmt.Errorf("lighter: failed to parse position size %q for symbol %s (account_index=%d): %w", p.Position, p.Symbol, accountIndex, err)
				}
				if math.Abs(size) < 0.000000001 {
					continue
				}
				balances = append(balances, &models.BalanceSnapshot{
					Asset:       p.Symbol,
					Balance:     cleanDecimal(p.Position),
					TimestampMs: nowMs,
					WalletType:  "perp",
				})
			}

			// If we emitted nothing (zero collateral and no open positions),
			// return nil so the snapshot syncer's empty-balances path writes
			// zero snapshots for previously-seen assets.
			if len(balances) == 0 {
				return nil, nil
			}
			return balances, nil
		}

		// Main account (account_type=0): use per-asset balances tagged
		// wallet_type=spot.
		for _, a := range acct.Assets {
			bal, err := strconv.ParseFloat(a.Balance, 64)
			if err != nil {
				return nil, fmt.Errorf("lighter: failed to parse balance %q for asset %s (account_index=%d): %w", a.Balance, a.Symbol, accountIndex, err)
			}
			if math.Abs(bal) < 0.000001 {
				continue
			}
			_ = bal
			balances = append(balances, &models.BalanceSnapshot{
				Asset:       a.Symbol,
				Balance:     a.Balance,
				TimestampMs: nowMs,
				WalletType:  "spot",
			})
		}
	}

	return balances, nil
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
