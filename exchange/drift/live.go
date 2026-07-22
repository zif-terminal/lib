package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/zif-terminal/lib/models"
)

// Drift /user/{accountId} response types

type driftUserResponse struct {
	Balances []driftUserBalance `json:"balances"`
}

type driftUserBalance struct {
	Symbol      string `json:"symbol"`
	MarketIndex int    `json:"marketIndex"`
	Balance     string `json:"balance"`
}

// driftCandleRecord represents a single candle from /market/{symbol}/candles/{resolution}
type driftCandleRecord struct {
	Ts          int64   `json:"ts"`
	OracleOpen  float64 `json:"oracleOpen"`
	OracleHigh  float64 `json:"oracleHigh"`
	OracleClose float64 `json:"oracleClose"`
	OracleLow   float64 `json:"oracleLow"`
}

// driftCandlesResponse wraps the candles API response.
type driftCandlesResponse struct {
	Success bool                `json:"success"`
	Records []driftCandleRecord `json:"records"`
}

// stableAssets is the set of spot assets that are treated as 1.0 USD by
// definition. We do this because (a) Drift doesn't list a USDC-PERP or
// USDT-PERP candle to query, and (b) treating dollar-stablecoins as $1 is
// the convention every other exchange client in this codebase already uses
// (see hyperliquid/client.go FetchBalances USDC handling).
var stableAssets = map[string]struct{}{
	"USDC":   {},
	"USDT":   {},
	"USDC-4": {},
	"USDT-4": {},
	"USDY":   {},
	"EURC":   {}, // EURC is EUR-pegged, not USD; mark as no-price below instead
	"CASH":   {},
	"USD1":   {},
}

// spotAssetToPerpSymbol maps a Drift spot asset symbol to the perp symbol
// used by /market/{symbol}/candles. Drift's candles endpoint is the only
// reliable live-price source on data.api.drift.trade (the /stats/markets
// "currentPrice" field comes back empty in prod, /amm/oraclePrice rejects
// every query as success=false). Wrapped / variant spot tokens are mapped
// onto the underlying perp:
//
//	wBTC  -> BTC-PERP
//	wETH  -> ETH-PERP
//	cbBTC -> BTC-PERP
//	zBTC  -> BTC-PERP
//	LBTC  -> BTC-PERP
//	mSOL / jitoSOL / bSOL / dSOL / SOL-2 / JitoSOL-2 / JitoSOL-3 / dfdvSOL -> SOL-PERP
//
// Tokens with no candle equivalent (e.g. JLP, INF, BONK without a perp,
// PT-* points tokens, syrupUSDC, sACRED, BNSOL, META, ZEUS, MET) return ""
// and are written with nil price + a WARN log -- preserving the balance
// row without poisoning the dashboard sum.
func spotAssetToPerpSymbol(asset string) string {
	// Direct mappings for wrappers / variants
	switch asset {
	case "wBTC", "cbBTC", "zBTC", "LBTC":
		return "BTC-PERP"
	case "wETH":
		return "ETH-PERP"
	case "mSOL", "jitoSOL", "bSOL", "dSOL", "SOL-2", "JitoSOL-2", "JitoSOL-3", "dfdvSOL":
		return "SOL-PERP"
	case "JTO-2", "JTO-3":
		return "JTO-PERP"
	case "1KPUMP":
		// "1KPUMP" perp exists (idx 81). Keep as-is.
		return "1KPUMP-PERP"
	}
	// Default: assume the spot symbol matches a perp symbol.
	return asset + "-PERP"
}

// fetchSpotPrices resolves the oracle price (in USDC) for each requested
// spot asset. It returns a map[asset]priceString. Missing entries (e.g.
// exotic tokens without a candle endpoint) are silently absent -- the
// caller treats absence as "no price available" and writes the snapshot
// row with nil oracle_price/usd_value.
//
// Each non-stablecoin asset triggers one /market/{ASSET-PERP}/candles/D
// call. We anchor startTs at "now" and endTs at "now - 90d" (max API
// allows >2y in theory; 90d is comfortably wide given the data API
// occasionally lags). The first record returned is the most-recent
// available candle -- we take its oracleClose. Drift's data API is
// known to lag the live market by hours-to-days; for snapshot-pricing
// purposes that's an acceptable tradeoff (HL has the same property
// because spotMetaAndAssetCtxs is also a polled feed, not realtime).
//
// USDC and other dollar-pegged spot markets short-circuit to "1".
func (c *Client) fetchSpotPrices(ctx context.Context, assets []string) (map[string]string, error) {
	out := make(map[string]string, len(assets))

	// Dedupe assets while preserving order so logs are stable.
	seen := make(map[string]struct{}, len(assets))
	uniq := make([]string, 0, len(assets))
	for _, a := range assets {
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		uniq = append(uniq, a)
	}

	nowSec := time.Now().Unix()
	startTs := nowSec
	// 90-day window is wide enough to cover the Drift data API's typical lag.
	endTs := nowSec - int64(90*24*60*60)

	for _, asset := range uniq {
		if _, isStable := stableAssets[asset]; isStable {
			// EURC is EUR-pegged, not USD -- skip it so the row gets nil
			// price + a WARN rather than a fake $1 mark.
			if asset == "EURC" {
				log.Printf("drift/fetchSpotPrices: WARN no USD price for asset=%s (EUR-pegged stablecoin, no candle endpoint)", asset)
				continue
			}
			out[asset] = "1"
			continue
		}

		perpSym := spotAssetToPerpSymbol(asset)
		if perpSym == "" {
			log.Printf("drift/fetchSpotPrices: WARN no perp mapping for asset=%s -- snapshot row will have nil oracle_price", asset)
			continue
		}

		price, err := c.fetchLatestCandleOraclePrice(ctx, perpSym, startTs, endTs)
		if err != nil {
			// Pricing failure for one asset must not poison the entire
			// balance-fetch -- log and continue. The row will be written
			// with nil price.
			log.Printf("drift/fetchSpotPrices: WARN failed to fetch price for asset=%s perp=%s: %v", asset, perpSym, err)
			continue
		}
		if price == "" {
			log.Printf("drift/fetchSpotPrices: WARN empty price for asset=%s perp=%s -- snapshot row will have nil oracle_price", asset, perpSym)
			continue
		}
		out[asset] = price
	}

	return out, nil
}

// fetchLatestCandleOraclePrice fetches the most-recent candle for the given
// perp symbol and returns its oracleClose as a clean decimal string.
// Returns ("", nil) if no candle was found in the window.
func (c *Client) fetchLatestCandleOraclePrice(ctx context.Context, perpSymbol string, startTs, endTs int64) (string, error) {
	// Drift candles use D (daily) resolution -- it's the cheapest and the
	// data we get back is the same oracleClose regardless of resolution
	// when the market is dormant on the API side.
	url := fmt.Sprintf("%s/market/%s/candles/D?startTs=%d&endTs=%d&limit=1", c.baseURL, perpSymbol, startTs, endTs)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("candles API returned status %d for %s", resp.StatusCode, perpSymbol)
	}

	var result driftCandlesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode candles response for %s: %w", perpSymbol, err)
	}

	if !result.Success || len(result.Records) == 0 {
		return "", nil
	}

	// Records are returned newest-first; the first entry is the latest
	// candle. oracleClose is the oracle's mark at the close of that
	// candle, which is what we want for a snapshot row.
	first := result.Records[0]
	if first.OracleClose <= 0 {
		return "", nil
	}
	return strconv.FormatFloat(first.OracleClose, 'f', -1, 64), nil
}

// Earn snapshots response types (used by FetchHistoricalBalanceSnapshots)

type driftEarnSnapshot struct {
	EpochTs int64            `json:"ts"`
	Assets  []driftEarnAsset `json:"assets"`
}

type driftEarnAsset struct {
	Symbol      string `json:"symbol"`
	MarketIndex int    `json:"marketIndex"`
	Balance     string `json:"balance"`
}

type driftEarnAccountSnapshot struct {
	AccountID string              `json:"accountId"`
	Snapshots []driftEarnSnapshot `json:"snapshots"`
}

type driftEarnResponse struct {
	Success  bool                       `json:"success"`
	Accounts []driftEarnAccountSnapshot `json:"accounts"`
}

// fetchUserAccount fetches per-account balance data
func (c *Client) fetchUserAccount(ctx context.Context, accountID string) (*driftUserResponse, error) {
	url := fmt.Sprintf("%s/user/%s", c.baseURL, accountID)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result driftUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// discoverAccountIDs discovers subaccount IDs for a wallet
func (c *Client) discoverAccountIDs(ctx context.Context, wallet string) ([]driftAuthorityAccount, error) {
	url := fmt.Sprintf("%s/authority/%s/accounts", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result driftAuthorityAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Accounts, nil
}

// getWalletFromAccount extracts the wallet address from the account
// For Drift, the AccountIdentifier is the subaccount pubkey, but we need the wallet authority
func (c *Client) getWalletFromAccount(account *models.ExchangeAccount) string {
	if account.AccountTypeMetadata != nil {
		var metadata map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &metadata); err == nil {
			if wallet, ok := metadata["authority"].(string); ok && wallet != "" {
				return wallet
			}
			if wallet, ok := metadata["wallet"].(string); ok && wallet != "" {
				return wallet
			}
		}
	}
	return ""
}

// FetchBalances fetches current spot balances from Drift.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}
	c.principal = accountID
	defer func() { c.principal = "" }()

	userData, err := c.fetchUserAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user data: %w", err)
	}
	if userData == nil {
		return []*models.BalanceSnapshot{}, nil
	}

	// Filter to non-dust balances first so we don't waste candle calls on
	// zero-balance rows.
	type kept struct {
		asset   string
		balance string
		amount  float64
	}
	var keep []kept
	assetSet := make([]string, 0, len(userData.Balances))
	seen := make(map[string]struct{}, len(userData.Balances))
	for _, b := range userData.Balances {
		amount := toFloat(b.Balance)
		if math.Abs(amount) < 0.000001 {
			continue
		}
		keep = append(keep, kept{asset: b.Symbol, balance: b.Balance, amount: amount})
		if _, ok := seen[b.Symbol]; !ok {
			seen[b.Symbol] = struct{}{}
			assetSet = append(assetSet, b.Symbol)
		}
	}

	// Resolve a price per distinct asset. Failures inside fetchSpotPrices
	// are swallowed (logged) -- we still write the row with nil price for
	// any asset whose price we couldn't resolve. This mirrors the
	// Hyperliquid pattern (see hyperliquid/client.go FetchBalances where
	// missing spot mark prices fall through to a nil-price snapshot).
	prices, err := c.fetchSpotPrices(ctx, assetSet)
	if err != nil {
		// fetchSpotPrices currently never returns an error (it logs and
		// continues), but defensively propagate if that changes -- a
		// systemic price-fetch failure is preferable to silent dollar
		// undercounting on the dashboard.
		return nil, fmt.Errorf("failed to fetch spot prices: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	var balances []*models.BalanceSnapshot
	for _, k := range keep {
		snap := &models.BalanceSnapshot{
			Asset:       k.asset,
			Balance:     k.balance,
			TimestampMs: nowMs,
		}
		if px, ok := prices[k.asset]; ok && px != "" {
			pxF, perr := strconv.ParseFloat(px, 64)
			if perr != nil || pxF <= 0 {
				log.Printf("drift/FetchBalances: WARN unusable price %q for asset=%s -- writing nil price", px, k.asset)
			} else {
				usd := k.amount * pxF
				usdStr := strconv.FormatFloat(usd, 'f', -1, 64)
				pxCopy := px
				snap.OraclePrice = &pxCopy
				snap.UsdValue = &usdStr
			}
		}
		balances = append(balances, snap)
	}

	return balances, nil
}

// fetchEarnResponse returns the earn snapshots payload for the given URL,
// serving a memoized copy when another sub-account of the same wallet fetched
// it within the TTL. On a cache miss it performs the live fetch and stores the
// successful result.
//
// The earn endpoint is wallet-scoped and returns snapshots for ALL sub-accounts
// under the wallet, so the payload is identical for every sub-account — the
// per-account filtering happens in the caller. The returned *driftEarnResponse
// is treated as read-only by callers (they only range over it), so sharing the
// pointer across sub-accounts is safe. Failures (transport error, non-200,
// success=false) are never cached, so a transient error is retried by the next
// sub-account rather than memoized.
func (c *Client) fetchEarnResponse(ctx context.Context, url string) (*driftEarnResponse, error) {
	if c.earnCache != nil {
		if cached, ok := c.earnCache.get(url); ok {
			return cached, nil
		}
	}

	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("earn snapshots returned status %d", resp.StatusCode)
	}

	var result driftEarnResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode earn response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("earn snapshots returned success=false")
	}

	if c.earnCache != nil {
		c.earnCache.set(url, &result)
	}
	return &result, nil
}

// FetchHistoricalBalanceSnapshots returns all historical earn snapshots from Drift.
// Each entry represents a point-in-time snapshot of all asset balances.
// Returns snapshots sorted by timestamp ascending (oldest first).
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	wallet := c.getWalletFromAccount(account)
	if wallet == "" {
		return nil, fmt.Errorf("wallet address required for historical snapshots")
	}

	accountID := account.AccountIdentifier
	c.principal = accountID
	defer func() { c.principal = "" }()

	url := fmt.Sprintf("%s/authority/%s/snapshots/earn", c.baseURL, wallet)
	result, err := c.fetchEarnResponse(ctx, url)
	if err != nil {
		return nil, err
	}

	_ = c.fetchMarkets(ctx)

	for _, acct := range result.Accounts {
		if acct.AccountID != accountID {
			continue
		}

		var snapshots []*models.HistoricalBalanceSnapshots
		// Iterate in reverse so we return oldest-first
		for i := len(acct.Snapshots) - 1; i >= 0; i-- {
			snap := acct.Snapshots[i]
			var balances []*models.BalanceSnapshot
			for _, asset := range snap.Assets {
				balance := toFloat(asset.Balance)
				if math.Abs(balance) < 0.000001 {
					continue
				}

				symbol := asset.Symbol
				if symbol == "" {
					if info, ok := c.marketCache.getMarket(asset.MarketIndex, "spot"); ok {
						symbol = info.BaseAsset
					}
				}
				if symbol == "" {
					continue
				}

				_ = balance
				balances = append(balances, &models.BalanceSnapshot{
					Asset:   symbol,
					Balance: asset.Balance,
				})
			}

			if len(balances) > 0 {
				snapshots = append(snapshots, &models.HistoricalBalanceSnapshots{
					TimestampMs: snap.EpochTs * 1000, // earn API returns epoch seconds
					Balances:    balances,
				})
			}
		}

		return snapshots, nil
	}

	return nil, nil
}

// toFloat converts various types to float64
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0
		}
		return f
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
