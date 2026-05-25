package solana_dex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/exchange/httpcache"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// solana_dex/client.go
//
// Implements iface.ExchangeClient for a single Solana wallet, ingesting DEX
// swap activity (Jupiter / Raydium / Orca / etc.) via Helius's parsed
// enhanced-transactions API. The client is intentionally a thin wrapper over
// Helius — we lean on Helius's parsed `tokenTransfers` and `tokenBalanceChanges`
// rather than reinventing instruction decoding.
//
// Drift dedup (refined): Drift's own sync (`lib/exchange/drift`) records
// trades / deposits / withdraws / settlements on the *Drift sub-account*
// side. But when the WALLET (the authority) deposits USDC/SOL into a
// sub-account, the wallet's own ATA truly loses balance and that loss MUST
// be recorded against the solana_dex (wallet-side) account — otherwise the
// wallet appears to retain balances that have actually moved into Drift.
//
// Therefore the dedup rule is *finer-grained* than "skip any Drift tx":
//
//   - SWAP-typed activity inside a Drift tx (rare; e.g. Drift JIT routed
//     through a DEX): SKIP — Drift sync owns the trade record.
//   - Native/SPL transfer legs inside a Drift tx where the wallet is the
//     SOURCE (and the counterparty is not the wallet itself) → EMIT a
//     `TypeWithdraw` from solana_dex. Drift sync emits the matching INFLOW
//     on the sub-account side; together they net to 0 across the wallet.
//   - Native/SPL transfer legs inside a Drift tx where the wallet is the
//     DESTINATION → EMIT a `TypeDeposit` (wallet receiving USDC withdrawn
//     from a sub-account). Drift sync emits the matching OUTFLOW on the
//     sub-account.
//   - Pure Drift program calls (perp orders, liquidations, settlements,
//     funding accruals) with NO wallet-balance-affecting transfer leg →
//     continue to skip entirely.
//
// The DBClient is used to look up sibling Drift sub-account on-chain
// identifiers under the same wallet for documentation/defence-in-depth; the
// per-leg rule above is the source of truth.

// driftProgramID is the canonical Drift v2 clearing-house mainnet program id.
// Any Helius tx whose top-level instructions reference this program is the
// responsibility of the Drift sync — solana_dex must not emit anything for
// it, otherwise we double-count Drift deposits/withdraws/perps/settlements.
const driftProgramID = "dRiftyHA39MWEi3m9aunc5MzRF1JYuBsbn6VPcn33UH"

// driftSignerProgramIDs is a small allowlist of programs that Drift wraps
// around (e.g. JIT proxy, vaults). The cheapest correct signal is the main
// program id; we do not currently need this list, but leave it ready to
// extend if we ever see a Drift swap routed through one of these without
// touching the main program at the top level.
var driftSignerProgramIDs = map[string]struct{}{
	driftProgramID: {},
}

const (
	defaultBaseURL = "https://api.helius.xyz"
	httpTimeout    = 30 * time.Second
	// maxRetries is the number of times liveGet will re-attempt a 429'd
	// request. Capped at 3 (May 2026 credit-audit task #55): failed pages
	// resume on the next sync cycle anyway, so burning 8 retries (each of
	// which costs credits) only deepens an in-progress storm without making
	// progress. 3 retries is enough to absorb a transient gateway blip
	// without escalating into a doomed-retry loop.
	maxRetries     = 3
	initialBackoff = 2 * time.Second
	// maxBackoff caps single-attempt waits. Set to 5 minutes so the absolute
	// upper bound on a stuck request is ~maxRetries*maxBackoff (~40 minutes)
	// — but the typical case is far less because Retry-After (when present)
	// short-circuits the exponential schedule, and successful retries reset
	// the attempt counter.
	maxBackoff = 5 * time.Minute
	// backoffJitter is the maximum random delay added on top of the
	// exponential schedule. Keeps concurrent retries from synchronising.
	backoffJitter = 1 * time.Second

	// Jupiter price API. Public, no auth, rate-limited but generous.
	// We only call it from FetchBalances and only with the mints we
	// actually hold, so volume is negligible (one batched GET per
	// snapshot cycle per wallet, with a 5-minute in-process cache).
	defaultJupiterPriceURL = "https://lite-api.jup.ag/price/v3"
	jupiterPriceCacheTTL   = 5 * time.Minute
)

// Client implements iface.ExchangeClient for Solana DEX activity via Helius.
type Client struct {
	baseURL    string
	httpClient *http.Client
	limiter    *rateLimiter // nil → use globalLimiter
	apiKey     string       // resolved from account metadata at fetch-time
	transport  httpcache.Transport
	// principal scopes the httpcache key per-wallet. Set at the top of
	// every Fetch* method, cleared on return.
	principal string

	// dbClient is optional. When set, the client looks up sibling Drift
	// sub-account addresses under the same wallet to suppress mirror
	// outflows. nil → skip the sibling-lookup branch (program-id check
	// is still applied).
	dbClient db.DBClient

	// jupiterPriceURL is the Jupiter price endpoint. Tests override this to
	// point at httptest. Empty → defaultJupiterPriceURL.
	jupiterPriceURL string

	// priceCacheMu guards priceCache. We use a single struct-valued cache
	// per Client because FetchBalances is the only call site and a wallet
	// rarely holds more than ~30 distinct mints.
	priceCacheMu sync.Mutex
	priceCache   map[string]cachedPrice

	// txCache de-dupes Helius pagination between FetchTrades and FetchDeposits
	// within a single sync cycle. The SyncManager builds one Client per
	// exchange-sync-cycle and reuses it across all accounts of that exchange.
	// Both FetchTrades and FetchDeposits independently walk the same
	// per-wallet address list against the same Helius endpoint with the same
	// `until` window — without this cache the second call would re-issue
	// every page request and double-bill the Helius credit budget. (Credit
	// audit task #55, May 2026.)
	//
	// Key: txCacheKey{wallet, until}. Value: deduped transaction slice
	// returned by fetchAllTransactionsMulti. The same Client is reused for
	// multiple accounts so we MUST key by wallet, not just until.
	//
	// Lifetime: bounded by the Client's lifetime (one sync cycle). The
	// manager creates a new client every cycle so stale entries cannot
	// leak across cycles. Within a cycle, the cache is read-mostly: each
	// (wallet, until) sees at most two writers (FetchTrades + FetchDeposits
	// when their `since` values happen to coincide).
	txCacheMu sync.Mutex
	txCache   map[txCacheKey][]heliusTransaction
}

// txCacheKey is the (wallet, until) tuple used to memoise pagination results
// within a single sync cycle. See Client.txCache.
type txCacheKey struct {
	wallet string
	until  int64
}

// cachedPrice is one (mint → usdPrice) entry kept for jupiterPriceCacheTTL.
type cachedPrice struct {
	price    float64
	cachedAt time.Time
}

// NewClient creates a Solana DEX client without a DBClient (no sibling
// Drift sub-account lookup). Suitable for unit tests.
func NewClient() *Client {
	tr, err := httpcache.NewFromEnv()
	if err != nil {
		log.Printf("solana_dex: httpcache disabled (%v) — falling back to passthrough", err)
		tr = httpcache.Passthrough()
	}
	return &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
		transport:  tr,
	}
}

// NewClientWithDB creates a Solana DEX client that uses the provided DBClient
// to resolve sibling Drift sub-accounts under the same wallet. This is the
// production constructor — see exchange/client.go for wiring.
func NewClientWithDB(dbClient db.DBClient) *Client {
	c := NewClient()
	c.dbClient = dbClient
	return c
}

// Name returns the exchange identifier.
func (c *Client) Name() string {
	return "solana_dex"
}

// getAPIKey extracts the Helius API key from an exchange account's metadata.
// The metadata schema is documented on the new account row:
//   {"api_key":"...","wallet_address":"..."}
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

// getWalletAddress returns the wallet address to query. Prefers the explicit
// `wallet_address` metadata field (it can disambiguate cases where account_
// identifier is repurposed) and falls back to AccountIdentifier.
func getWalletAddress(account *models.ExchangeAccount) string {
	if account == nil {
		return ""
	}
	if account.AccountTypeMetadata != nil {
		var meta map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &meta); err == nil {
			if addr, ok := meta["wallet_address"].(string); ok && addr != "" {
				return addr
			}
		}
	}
	return account.AccountIdentifier
}

// doGet performs a GET request with rate limiting and retry on 429. When the
// httpcache transport is enabled and a principal is set, the cache is
// consulted before the live HTTP path runs.
func (c *Client) doGet(ctx context.Context, url string) (json.RawMessage, error) {
	if c.transport != nil && c.transport.IsEnabled() && c.principal != "" {
		body, err := c.transport.Get(ctx, "solana_dex", url, c.principal, func(ctx context.Context) ([]byte, error) {
			return c.liveGet(ctx, url)
		})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}
	return c.liveGet(ctx, url)
}

// liveGet is the original live-HTTP path with rate-limit + retry, separated
// so the cache transport can call it as its upstream Fetcher without
// re-entering the cache.
//
// Retry policy: on HTTP 429, wait for max(Retry-After header, jittered
// exponential backoff) and try again, up to maxRetries times. Respecting
// Retry-After is critical because Helius sometimes returns explicit
// retry-after hints longer than our exponential schedule would otherwise
// wait; following its hint avoids hammering the server during a known
// throttle window. Jitter on the exponential schedule prevents synchronised
// retries from concurrent goroutines (and back-to-back 429s on the same
// goroutine) from arriving in lockstep.
func (c *Client) liveGet(ctx context.Context, url string) (json.RawMessage, error) {
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

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			if attempt == maxRetries {
				wait := retryAfter
				if wait == 0 {
					wait = maxBackoff
				}
				return nil, &iface.RateLimitError{
					Exchange:   "solana_dex",
					Message:    fmt.Sprintf("rate limit exceeded after %d retries", maxRetries),
					RetryAfter: wait,
				}
			}
			// Honor Retry-After when set; otherwise jittered exponential.
			wait := backoffWithJitter(attempt, initialBackoff, backoffJitter, maxBackoff)
			if retryAfter > wait {
				wait = retryAfter
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// 404 → no data (legitimate for an unknown wallet, but Helius
		// generally returns [] not 404 for valid addresses).
		if resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("solana_dex: API status %d: %s", resp.StatusCode, string(body))
		}

		return body, nil
	}
	return nil, fmt.Errorf("max retries exceeded")
}

// parseRetryAfter parses the Retry-After header. The HTTP spec allows either
// a delta-seconds integer ("30") or an HTTP-date; in practice Helius uses
// delta-seconds, so that is what we parse. Empty / unparseable → 0 (caller
// falls back to its own backoff schedule).
func parseRetryAfter(retryAfter string) time.Duration {
	retryAfter = strings.TrimSpace(retryAfter)
	if retryAfter == "" {
		return 0
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// driftSubAccountIdentifiersForWallet looks up the on-chain addresses of the
// caller's Drift sub-accounts under the same wallet, so we can suppress
// transfers whose destination is a known sub-account address. This is a
// defence-in-depth: the program-id check should already catch every Drift
// tx, but if Helius ever surfaces a system-program-only transfer that
// happens to bridge into a Drift PDA, this catches it. Empty / error → "no
// known siblings" (no false suppression).
func (c *Client) driftSubAccountIdentifiersForWallet(ctx context.Context, walletID string) map[string]bool {
	out := map[string]bool{}
	if c.dbClient == nil || walletID == "" {
		return out
	}
	accounts, err := c.dbClient.ListAccounts(ctx, db.AccountListFilter{})
	if err != nil {
		return out
	}
	for _, a := range accounts {
		if a == nil || a.Exchange == nil {
			continue
		}
		if a.Exchange.Name != "drift" {
			continue
		}
		if a.WalletID == nil || *a.WalletID != walletID {
			continue
		}
		if a.AccountIdentifier != "" {
			out[a.AccountIdentifier] = true
		}
	}
	return out
}

// isDriftTransaction returns true if any top-level instruction references a
// Drift program id (or Helius tagged source=DRIFT).
//
// FetchTrades uses this to skip the SWAP→TradeInput conversion (Drift sync
// owns trade records). FetchDeposits uses this to switch into "bridge" mode:
// emit wallet-balance-affecting native/SPL legs but tag them with a
// `-bridge-(in|out)-l<n>` ExternalID so downstream queries can identify them
// (and so they can never collide with non-bridge leg ids from the same sig).
// See the package docstring for the full bridging-accounting rationale.
func isDriftTransaction(tx heliusTransaction) bool {
	for _, ins := range tx.Instructions {
		if _, ok := driftSignerProgramIDs[ins.ProgramID]; ok {
			return true
		}
	}
	// Helius also tags the source field directly. We trust source=DRIFT
	// as a secondary signal, but never as primary — the program-id check
	// is the source of truth. (If Helius mis-tags source for a non-Drift
	// JUP swap, we'd silently drop it; the program-id check avoids that.)
	if tx.Source == "DRIFT" {
		return true
	}
	return false
}

// fetchWalletATAs queries Helius /v0/addresses/<wallet>/balances and returns
// the list of the wallet's ATA token-account addresses that currently hold a
// non-zero balance. These are the addresses we additionally query against
// /v0/addresses/<ata>/transactions to catch inflows (notably multi-out
// airdrops) where Helius's wallet-address tx index doesn't return the tx
// because the wallet authority isn't in the tx's `accountData[].account`
// list — only the recipient ATA is.
//
// We filter to non-zero balances so a wallet with 100 stale ATAs doesn't
// fan out to 101 transaction-queries per cycle. Dust mints that paid out
// once and emptied may still be missed if they're a zero balance NOW; that
// trade-off is intentional under the rate-limit constraint and is
// documented in the issue body.
//
// On any error (network, decode), returns nil with no error: the caller
// falls back to the wallet-only query (no regression vs prior behaviour).
func (c *Client) fetchWalletATAs(ctx context.Context, walletAddr string) []string {
	u := fmt.Sprintf("%s/v0/addresses/%s/balances?api-key=%s", c.baseURL, walletAddr, c.apiKey)
	body, err := c.doGet(ctx, u)
	if err != nil || body == nil {
		return nil
	}
	var resp heliusBalancesResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	out := make([]string, 0, len(resp.Tokens))
	for _, t := range resp.Tokens {
		if t.Amount == 0 {
			continue
		}
		if t.TokenAccount == "" {
			continue
		}
		out = append(out, t.TokenAccount)
	}
	return out
}

// addressesForTxQuery returns the list of Solana addresses we query against
// Helius's enhanced-tx endpoint per sync cycle: the wallet itself plus each
// non-zero ATA. See fetchWalletATAs for the rationale.
func (c *Client) addressesForTxQuery(ctx context.Context, walletAddr string) []string {
	addrs := []string{walletAddr}
	addrs = append(addrs, c.fetchWalletATAs(ctx, walletAddr)...)
	return addrs
}

// fetchTransactionsForCycle returns the deduped transaction list for the given
// (wallet, until) within the current sync cycle. The first caller in the cycle
// hits Helius via fetchAllTransactionsMulti; subsequent callers with the same
// key read from c.txCache and skip the paginator entirely.
//
// This eliminates the duplicate-pagination cost between FetchTrades and
// FetchDeposits, which were independently walking the same address list with
// the same `until` window. See Client.txCache for the lifetime contract.
//
// If addresses changes between the two callers (e.g. an ATA was opened
// mid-cycle), the SECOND caller's address list is the one that wins on a
// cache miss. On a cache hit we trust the first caller's snapshot — the small
// risk of missing an ATA that only appeared between the two Fetch* calls is
// preferred to doubling our Helius credit spend every cycle.
func (c *Client) fetchTransactionsForCycle(
	ctx context.Context,
	walletAddr string,
	addresses []string,
	untilTimestamp int64,
) ([]heliusTransaction, error) {
	key := txCacheKey{wallet: walletAddr, until: untilTimestamp}

	c.txCacheMu.Lock()
	if c.txCache != nil {
		if cached, ok := c.txCache[key]; ok {
			c.txCacheMu.Unlock()
			return cached, nil
		}
	}
	c.txCacheMu.Unlock()

	txs, err := fetchAllTransactionsMulti(ctx, c, addresses, untilTimestamp)
	if err != nil {
		// Do not cache failures: the next caller should retry.
		return nil, err
	}

	c.txCacheMu.Lock()
	if c.txCache == nil {
		c.txCache = make(map[txCacheKey][]heliusTransaction)
	}
	c.txCache[key] = txs
	c.txCacheMu.Unlock()

	return txs, nil
}

// FetchTrades fetches DEX swaps for the wallet and converts each to a
// TradeInput. One Helius SWAP tx → one TradeInput row (we collapse
// multi-hop legs into a single trade representing the wallet's net in/out).
//
// Drift txs are skipped at the trade level (Drift sync owns trade records),
// but their wallet-balance-affecting transfer legs are emitted by
// FetchDeposits — see the package docstring on bridging accounting.
// Failed txs (transactionError != null) are skipped — they didn't affect
// balances.
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

	apiKey := getAPIKey(account)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("solana_dex: api_key missing from account metadata")
	}
	c.apiKey = apiKey

	walletAddr := getWalletAddress(account)
	if walletAddr == "" {
		return nil, nil, fmt.Errorf("solana_dex: wallet address missing")
	}
	c.principal = walletAddr
	defer func() { c.principal = "" }()

	until := int64(0)
	if !since.IsZero() {
		until = since.Unix()
	}

	addrs := c.addressesForTxQuery(ctx, walletAddr)
	txs, err := c.fetchTransactionsForCycle(ctx, walletAddr, addrs, until)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}

	trades := make([]*models.TradeInput, 0)
	prices := make([]*models.PriceRecord, 0)
	sinceMs := since.UnixMilli()

	for _, tx := range txs {
		if tx.TxError != nil {
			continue
		}
		// Drift dedup: the trade/swap record (if any) belongs to Drift's sync,
		// even if Helius mis-tags a Drift JIT fill as `SWAP`. We still process
		// transfer legs from Drift txs in FetchDeposits (bridging accounting).
		if isDriftTransaction(tx) {
			continue
		}
		if tx.Type != "SWAP" {
			continue
		}
		ts := time.Unix(tx.Timestamp, 0).UTC()
		if !since.IsZero() && ts.UnixMilli() < sinceMs {
			continue
		}

		trade, price, ok := buildSwapTrade(tx, walletAddr, accountUUID)
		if !ok {
			continue
		}
		trades = append(trades, trade)
		if price != nil {
			prices = append(prices, price)
		}
	}

	sort.Slice(trades, func(i, j int) bool { return trades[i].Timestamp.Before(trades[j].Timestamp) })
	sort.Slice(prices, func(i, j int) bool { return prices[i].Timestamp.Before(prices[j].Timestamp) })

	return trades, prices, nil
}

// netDelta is the per-(mint) net change for the wallet across a tx.
type netDelta struct {
	mint    string
	amount  *big.Rat // signed: positive = wallet received, negative = wallet sent
}

// walletNetDeltas computes net token-balance changes for the wallet across a
// Helius transaction by summing tokenBalanceChanges where userAccount==wallet.
// Native SOL deltas are returned as a separate value (lamports).
//
// We use raw integer rawTokenAmount values via big.Rat so amounts retain
// full precision (the float64 in tokenTransfers is lossy for small mints).
func walletNetDeltas(tx heliusTransaction, walletAddr string) (tokens []netDelta, nativeLamports int64) {
	// SPL token deltas: aggregated by mint across all wallet-owned token accounts.
	byMint := make(map[string]*big.Rat)
	for _, ad := range tx.AccountData {
		for _, tbc := range ad.TokenBalanceChanges {
			if tbc.UserAccount != walletAddr {
				continue
			}
			n, ok := new(big.Int).SetString(tbc.RawTokenAmount.TokenAmount, 10)
			if !ok {
				continue
			}
			// Convert to a decimal rational: n / 10^decimals.
			num := new(big.Int).Set(n)
			den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tbc.RawTokenAmount.Decimals)), nil)
			amt := new(big.Rat).SetFrac(num, den)
			if cur, ok := byMint[tbc.Mint]; ok {
				cur.Add(cur, amt)
			} else {
				byMint[tbc.Mint] = amt
			}
		}
	}
	for mint, amt := range byMint {
		// Skip dust: rationals with numerator 0.
		if amt.Sign() == 0 {
			continue
		}
		tokens = append(tokens, netDelta{mint: mint, amount: amt})
	}

	// Native SOL delta: take the wallet's nativeBalanceChange entry.
	for _, ad := range tx.AccountData {
		if ad.Account == walletAddr {
			nativeLamports = ad.NativeBalanceChange
			break
		}
	}

	return tokens, nativeLamports
}

// buildSwapTrade collapses a Helius SWAP tx into a single TradeInput by:
//   1. Computing the wallet's net per-mint delta for the tx.
//   2. Picking the largest-magnitude negative delta as the "sold" leg
//      (base when side=sell, quote when side=buy depending on what's
//      stable).
//   3. Picking the largest-magnitude positive delta as the "received" leg.
//   4. Treating USDC/USDT/PYUSD as the quote when one side is a stable.
//      Otherwise we prefer the larger leg as base and call the other quote.
//
// Returns ok=false when the tx doesn't have a clear in/out pair (e.g.
// arbitrage that nets to zero, or a multi-hop where the wallet is only an
// intermediate signer). We drop these rather than synthesising a trade.
func buildSwapTrade(
	tx heliusTransaction,
	walletAddr string,
	accountUUID uuid.UUID,
) (*models.TradeInput, *models.PriceRecord, bool) {
	tokens, native := walletNetDeltas(tx, walletAddr)

	// Fold native SOL into the same delta list (if non-trivial — fee-only
	// changes appear as negative tens of thousands of lamports and should
	// not be misidentified as a swap leg).
	if abs64(native) > 100_000 { // >0.0001 SOL → real movement, not just fee
		// Native SOL delta is in lamports; convert to a rational SOL.
		amt := new(big.Rat).SetFrac(big.NewInt(native), big.NewInt(1_000_000_000))
		tokens = append(tokens, netDelta{mint: mintWSOL, amount: amt})
	}

	if len(tokens) < 2 {
		return nil, nil, false
	}

	// Find largest negative (sold) and largest positive (received) by abs value.
	var sold, recv *netDelta
	for i := range tokens {
		t := &tokens[i]
		if t.amount.Sign() < 0 {
			if sold == nil || ratAbsCmp(t.amount, sold.amount) > 0 {
				sold = t
			}
		} else if t.amount.Sign() > 0 {
			if recv == nil || ratAbsCmp(t.amount, recv.amount) > 0 {
				recv = t
			}
		}
	}
	if sold == nil || recv == nil {
		return nil, nil, false
	}

	soldSym := resolveMint(sold.mint)
	recvSym := resolveMint(recv.mint)
	soldAmtAbs := new(big.Rat).Abs(new(big.Rat).Set(sold.amount))
	recvAmt := new(big.Rat).Set(recv.amount)

	// Decide base/quote: stable-prefers-quote.
	side := "buy"
	base, quote := recvSym, soldSym
	baseQty := recvAmt
	quoteQty := soldAmtAbs
	switch {
	case isStable(soldSym) && !isStable(recvSym):
		// Sold stable → bought non-stable. base=non-stable, quote=stable, side=buy.
		side = "buy"
		base, quote = recvSym, soldSym
		baseQty, quoteQty = recvAmt, soldAmtAbs
	case isStable(recvSym) && !isStable(soldSym):
		// Sold non-stable → received stable. base=non-stable, quote=stable, side=sell.
		side = "sell"
		base, quote = soldSym, recvSym
		baseQty, quoteQty = soldAmtAbs, recvAmt
	default:
		// Neither / both stable. Prefer USDC > USDT > PYUSD for quote ordering;
		// otherwise treat the side as buy with received as base.
		if isStable(soldSym) && isStable(recvSym) {
			// Stable-to-stable: arbitrary base=recv, quote=sold, side=buy.
			side = "buy"
		} else {
			// Neither is stable: base=recv, quote=sold, side=buy.
			side = "buy"
		}
	}

	// Price = quote_qty / base_qty (as a decimal string).
	priceStr := "0"
	if baseQty.Sign() > 0 {
		p := new(big.Rat).Quo(quoteQty, baseQty)
		priceStr = ratToDecimalString(p, 12)
	}

	ts := time.Unix(tx.Timestamp, 0).UTC()

	trade := &models.TradeInput{
		BaseAsset:         base,
		QuoteAsset:        quote,
		Side:              side,
		Price:             priceStr,
		Quantity:          ratToDecimalString(baseQty, 12),
		Timestamp:         ts,
		Fee:               lamportsToSolDecimal(tx.Fee),
		FeeAsset:          "SOL",
		OrderID:           tx.Signature,
		TradeID:           tx.Signature,
		ExchangeAccountID: accountUUID,
		MarketType:        "spot",
		TxSignature:       tx.Signature,
	}

	var price *models.PriceRecord
	if priceStr != "0" {
		price = &models.PriceRecord{
			Asset:        base,
			Denomination: quote,
			Timestamp:    ts,
			Price:        priceStr,
			Source:       "execution",
		}
	}

	return trade, price, true
}

func isStable(sym string) bool {
	switch sym {
	case "USDC", "USDT", "PYUSD":
		return true
	}
	return false
}

// FetchDeposits fetches native/SPL transfers in/out of the wallet, including
// bridge-style transfers into/out of the wallet's own Drift sub-accounts.
// Inflows → TypeDeposit. Outflows → TypeWithdraw.
//
// Bridging accounting: Drift txs are NOT skipped wholesale. For a Drift tx,
// any wallet-balance-affecting leg is emitted (with ExternalID
// `<sig>-bridge-<dir>-l<n>`) so the wallet's true outflow on a deposit-into-
// Drift (or inflow on a withdraw-from-Drift) is recorded. Drift's own sync
// emits the matching leg on the sub-account side, so the two net to 0 across
// the wallet. Pure Drift program calls (perp orders, settlements) have no
// wallet-touching native/SPL legs and emit nothing.
//
// For non-Drift txs, outflows to addresses that match a known sibling Drift
// sub-account identifier (looked up via dbClient) are still suppressed —
// defence-in-depth in case Helius ever fails to tag a Drift tx.
//
// SWAP txs are excluded here — they're emitted as TradeInput by FetchTrades.
//
// External-source deposits don't carry an oracle price (we don't know the
// USD value of e.g. an inbound SOL transfer at tx time), so the second
// return value is always nil for now. Future enhancement could call a
// pricing oracle.
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

	apiKey := getAPIKey(account)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("solana_dex: api_key missing from account metadata")
	}
	c.apiKey = apiKey

	walletAddr := getWalletAddress(account)
	if walletAddr == "" {
		return nil, nil, fmt.Errorf("solana_dex: wallet address missing")
	}
	c.principal = walletAddr
	defer func() { c.principal = "" }()

	until := int64(0)
	if !since.IsZero() {
		until = since.Unix()
	}

	addrs := c.addressesForTxQuery(ctx, walletAddr)
	txs, err := c.fetchTransactionsForCycle(ctx, walletAddr, addrs, until)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}

	// ATA set used as a defence-in-depth membership fallback in
	// buildTransferLegs: if Helius doesn't resolve `toUserAccount` to the
	// wallet authority (rare, but observed on multi-out airdrops fetched via
	// /v0/addresses/<ata>/transactions), the wallet-owned ATA address still
	// identifies the wallet as the inflow recipient.
	walletATAs := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if a != "" && a != walletAddr {
			walletATAs[a] = true
		}
	}

	walletID := ""
	if account.WalletID != nil {
		walletID = *account.WalletID
	}
	driftSiblings := c.driftSubAccountIdentifiersForWallet(ctx, walletID)

	transfers := make([]*models.TransferInput, 0)
	sinceMs := since.UnixMilli()

	for _, tx := range txs {
		if tx.TxError != nil {
			continue
		}
		// Swaps are emitted as trades — even Drift-tagged swaps (handled in
		// FetchTrades by isDriftTransaction).
		if tx.Type == "SWAP" {
			continue
		}
		ts := time.Unix(tx.Timestamp, 0).UTC()
		if !since.IsZero() && ts.UnixMilli() < sinceMs {
			continue
		}

		// Bridging accounting: For Drift txs we DO NOT skip the whole tx.
		// Instead we still emit wallet-balance-affecting legs (so the
		// wallet's outflow on a deposit-into-Drift, or inflow on a
		// withdraw-from-Drift, is recorded). Drift's own sync emits the
		// matching leg on the sub-account side; the two net to 0 across
		// the wallet. Pure Drift program calls (perp orders, settlements)
		// produce no wallet-touching native/SPL legs and so emit nothing.
		isDrift := isDriftTransaction(tx)

		// Emit one transfer leg per native SOL movement involving the wallet
		// directly (not just fee-shaped deltas), and one per SPL token
		// transfer involving the wallet. Use signature+leg suffix as ExternalID.
		legs := buildTransferLegs(tx, walletAddr, walletATAs, accountUUID, driftSiblings, isDrift)
		transfers = append(transfers, legs...)
	}

	sort.Slice(transfers, func(i, j int) bool {
		if !transfers[i].Timestamp.Equal(transfers[j].Timestamp) {
			return transfers[i].Timestamp.Before(transfers[j].Timestamp)
		}
		return transfers[i].ExternalID < transfers[j].ExternalID
	})

	return transfers, nil, nil
}

// buildTransferLegs returns a TransferInput for every wallet-touching native
// SOL or SPL transfer in the tx (excluding self-transfers).
//
// Behaviour depends on whether this is a Drift tx:
//
//   - Non-Drift tx (isDrift=false): emit each wallet-touching leg.
//     Outflows whose destination is a known Drift sibling are suppressed
//     (defence-in-depth — keeps non-Drift code path unchanged from prior
//     behaviour, in case Helius ever mis-classifies a Drift tx).
//
//   - Drift tx (isDrift=true): emit ONE net leg per mint as a "bridge"
//     transfer, using walletNetDeltas to net out mirror-pair inflow/outflow
//     legs that Drift JIT transactions produce on the wallet's ATA within a
//     single signature. The wallet truly loses balance when depositing into
//     a Drift sub-account, so we record the net outflow here; Drift sync
//     records the matching inflow on the sub-account side. ExternalID is
//     suffixed with `-bridge-out` / `-bridge-in` so these rows are easy to
//     identify downstream and can never collide with non-Drift leg IDs from
//     the same signature.
//
// Why per-mint netting on the Drift path:
// Helius's `tokenTransfers` array surfaces every SPL transfer instruction in
// the tx, including inner instructions. For Drift JIT fills (and other
// in-flight rebalances) the wallet's ATA is touched on BOTH sides of the
// same signature for the same mint — once as the source of USDC paid in,
// once as the destination of USDC paid back. Iterating raw token transfers
// (which is what the pre-fix code did) emitted both legs, producing phantom
// mirror-pair deposit/withdraw rows that net to zero economically but each
// counted independently against FIFO inventory. `walletNetDeltas` instead
// sums the per-account `tokenBalanceChanges` entries owned by the wallet
// for the mint, yielding the correct net delta. Mirror pairs cancel and
// emit nothing; partial mirrors emit a single leg in the net direction.
func buildTransferLegs(
	tx heliusTransaction,
	walletAddr string,
	walletATAs map[string]bool,
	accountUUID uuid.UUID,
	driftSiblings map[string]bool,
	isDrift bool,
) []*models.TransferInput {
	var out []*models.TransferInput
	ts := time.Unix(tx.Timestamp, 0).UTC()
	legIdx := 0

	makeExternalID := func(direction string) string {
		if isDrift {
			// Use a distinct namespace so bridge legs are obvious in the
			// transfers table and can never collide with the non-bridge
			// leg-numbering scheme used for ordinary txs.
			return fmt.Sprintf("%s-bridge-%s-l%d", tx.Signature, direction, legIdx)
		}
		return fmt.Sprintf("%s-l%d", tx.Signature, legIdx)
	}

	makeMetadata := func(extra map[string]string) map[string]string {
		m := map[string]string{
			"tx_signature":  tx.Signature,
			"helius_type":   tx.Type,
			"helius_source": tx.Source,
		}
		for k, v := range extra {
			m[k] = v
		}
		if isDrift {
			m["bridge"] = "drift"
		}
		return m
	}

	// Drift path: per-mint netting via walletNetDeltas. Mirror-pair legs
	// cancel to zero and emit nothing; partial mirrors emit one net leg.
	// Native SOL nets via the wallet's nativeBalanceChange (lamports).
	if isDrift {
		tokens, nativeLamports := walletNetDeltas(tx, walletAddr)

		// Native SOL: only emit when there is a real movement, not a fee-
		// shaped delta. Matches buildSwapTrade's >0.0001 SOL threshold so
		// that wrap/unwrap/fee dust doesn't synthesise a phantom leg.
		if abs64(nativeLamports) > 100_000 {
			direction := "in"
			transferType := models.TypeDeposit
			amt := nativeLamports
			if amt < 0 {
				direction = "out"
				transferType = models.TypeWithdraw
				amt = -amt
			}
			out = append(out, &models.TransferInput{
				ExchangeAccountID: accountUUID,
				Type:              transferType,
				Asset:             "SOL",
				Amount:            lamportsToSolDecimal(amt),
				Timestamp:         ts,
				ExternalID:        makeExternalID(direction),
				Metadata: makeMetadata(map[string]string{
					"transfer_kind": "native",
				}),
			})
			legIdx++
		}

		// SPL token legs: one per mint with non-zero net delta. Sort mints
		// for deterministic leg ordering / ExternalID assignment.
		sort.Slice(tokens, func(i, j int) bool { return tokens[i].mint < tokens[j].mint })
		for _, td := range tokens {
			// Skip wSOL when the native leg above already covered it
			// (otherwise we'd double-count SOL inflow/outflow because
			// resolveMint(mintWSOL) == "SOL").
			if td.mint == mintWSOL && abs64(nativeLamports) > 100_000 {
				continue
			}
			sign := td.amount.Sign()
			if sign == 0 {
				continue
			}
			direction := "in"
			transferType := models.TypeDeposit
			amt := new(big.Rat).Set(td.amount)
			if sign < 0 {
				direction = "out"
				transferType = models.TypeWithdraw
				amt.Neg(amt)
			}
			out = append(out, &models.TransferInput{
				ExchangeAccountID: accountUUID,
				Type:              transferType,
				Asset:             resolveMint(td.mint),
				Amount:            ratToDecimalString(amt, 12),
				Timestamp:         ts,
				ExternalID:        makeExternalID(direction),
				Metadata: makeMetadata(map[string]string{
					"mint":          td.mint,
					"transfer_kind": "spl",
				}),
			})
			legIdx++
		}

		return out
	}

	// Native SOL legs.
	for _, nt := range tx.NativeTransfers {
		if nt.Amount <= 0 {
			continue
		}
		if nt.FromUserAccount != walletAddr && nt.ToUserAccount != walletAddr {
			continue
		}
		if nt.FromUserAccount == nt.ToUserAccount {
			continue // self
		}
		var transferType, direction, counter string
		switch {
		case nt.FromUserAccount == walletAddr:
			transferType = models.TypeWithdraw
			direction = "out"
			counter = nt.ToUserAccount
		case nt.ToUserAccount == walletAddr:
			transferType = models.TypeDeposit
			direction = "in"
			counter = nt.FromUserAccount
		}
		// Outside Drift txs, suppress outflows to known Drift sibling
		// addresses — Drift sync records that destination. Inside a Drift
		// tx we DO want the leg (that is the whole point of bridging).
		if !isDrift && transferType == models.TypeWithdraw && driftSiblings[counter] {
			continue
		}
		out = append(out, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              transferType,
			Asset:             "SOL",
			Amount:            lamportsToSolDecimal(nt.Amount),
			Timestamp:         ts,
			ExternalID:        makeExternalID(direction),
			Metadata: makeMetadata(map[string]string{
				"counterparty":  counter,
				"transfer_kind": "native",
			}),
		})
		legIdx++
	}

	// Wrapped-SOL dedup: when the wallet's native (lamport) balance changed
	// in this tx, the native leg above is the canonical accounting for the
	// economic event. Helius also surfaces a wSOL SPL token transfer for the
	// same movement (close-account/sync-native/wrap/unwrap, Drift-bridge wSOL
	// deposits). Because resolveMint(mintWSOL) == "SOL", emitting both would
	// double-count phantom SOL inflow/outflow on the same ledger. We therefore
	// drop wSOL token-transfer legs whenever the wallet had any non-zero
	// nativeBalanceChange in this tx — the native leg already captured it.
	walletHadNativeChange := false
	for _, ad := range tx.AccountData {
		if ad.Account == walletAddr && ad.NativeBalanceChange != 0 {
			walletHadNativeChange = true
			break
		}
	}

	// SPL token legs.
	for _, tt := range tx.TokenTransfers {
		// Skip wSOL token legs when the native leg above already accounts
		// for this movement (see walletHadNativeChange comment).
		if walletHadNativeChange && tt.Mint == mintWSOL {
			continue
		}
		// Primary check: wallet authority is named as from/to user. Defence-
		// in-depth fallback: when fetched via /v0/addresses/<ata>/transactions
		// (multi-out airdrops), Helius may not resolve toUserAccount to the
		// wallet authority, but the destination ATA is still a wallet-owned
		// token account — treat that as a wallet inflow.
		isWalletFromUser := tt.FromUserAccount == walletAddr
		isWalletToUser := tt.ToUserAccount == walletAddr
		isWalletFromATA := walletATAs[tt.FromTokenAccount]
		isWalletToATA := walletATAs[tt.ToTokenAccount]
		if !isWalletFromUser && !isWalletToUser && !isWalletFromATA && !isWalletToATA {
			continue
		}
		if tt.FromUserAccount == tt.ToUserAccount && tt.FromUserAccount != "" {
			continue
		}
		if tt.TokenAmount == 0 {
			continue
		}
		var transferType, direction, counter string
		switch {
		case isWalletFromUser || isWalletFromATA:
			transferType = models.TypeWithdraw
			direction = "out"
			counter = tt.ToUserAccount
			if counter == "" {
				counter = tt.ToTokenAccount
			}
		case isWalletToUser || isWalletToATA:
			transferType = models.TypeDeposit
			direction = "in"
			counter = tt.FromUserAccount
			if counter == "" {
				counter = tt.FromTokenAccount
			}
		}
		if !isDrift && transferType == models.TypeWithdraw && driftSiblings[counter] {
			continue
		}
		out = append(out, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              transferType,
			Asset:             resolveMint(tt.Mint),
			Amount:            float64ToDecimal(tt.TokenAmount),
			Timestamp:         ts,
			ExternalID:        makeExternalID(direction),
			Metadata: makeMetadata(map[string]string{
				"counterparty":  counter,
				"mint":          tt.Mint,
				"transfer_kind": "spl",
			}),
		})
		legIdx++
	}
	return out
}

// FetchFundingPayments returns nil for solana_dex (DEX swaps don't pay funding).
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, nil
}

// FetchSettlements returns nil — DEX swaps settle on-chain immediately and
// the exchange row is configured with settles_on_close=true.
func (c *Client) FetchSettlements(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.Settlement, error) {
	return nil, nil
}

// FetchHistoricalBalanceSnapshots returns nil — we don't backfill historical
// snapshots for Solana wallets (Helius doesn't expose a per-second ledger).
// Current balances are captured each cycle by FetchBalances.
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	return nil, nil
}

// FetchBalances returns the wallet's current spot balance per asset.
// Calls Helius /v0/addresses/<addr>/balances and resolves each non-zero mint
// via resolveMint. Per-asset USD pricing is then layered on via a single
// batched call to Jupiter's free price API; assets Jupiter doesn't price
// (illiquid / delisted mints) keep oracle_price/usd_value nil so the
// snapshot syncer treats them as "unknown" rather than "$0".
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	apiKey := getAPIKey(account)
	if apiKey == "" {
		return nil, fmt.Errorf("solana_dex: api_key missing from account metadata")
	}
	c.apiKey = apiKey

	walletAddr := getWalletAddress(account)
	if walletAddr == "" {
		return nil, fmt.Errorf("solana_dex: wallet address missing")
	}
	c.principal = walletAddr
	defer func() { c.principal = "" }()

	u := fmt.Sprintf("%s/v0/addresses/%s/balances?api-key=%s", c.baseURL, walletAddr, apiKey)
	body, err := c.doGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balances: %w", err)
	}
	if body == nil {
		return nil, nil
	}

	var resp heliusBalancesResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode balances response: %w", err)
	}

	nowMs := time.Now().UnixMilli()

	// Carry the mint alongside each snapshot so we can price it after the
	// loop without re-deriving the mint from the asset symbol.
	type pendingBalance struct {
		snap *models.BalanceSnapshot
		mint string
		bal  *big.Rat
	}
	var pending []pendingBalance

	if resp.NativeBalance > 0 {
		bal := new(big.Rat).SetFrac(big.NewInt(resp.NativeBalance), big.NewInt(1_000_000_000))
		pending = append(pending, pendingBalance{
			snap: &models.BalanceSnapshot{
				Asset:       "SOL",
				Balance:     lamportsToSolDecimal(resp.NativeBalance),
				TimestampMs: nowMs,
			},
			mint: mintWSOL,
			bal:  bal,
		})
	}

	for _, t := range resp.Tokens {
		if t.Amount == 0 {
			continue
		}
		// Convert raw integer amount to a decimal string using big.Rat.
		num := big.NewInt(t.Amount)
		den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.Decimals)), nil)
		bal := new(big.Rat).SetFrac(num, den)
		pending = append(pending, pendingBalance{
			snap: &models.BalanceSnapshot{
				Asset:       resolveMint(t.Mint),
				Balance:     ratToDecimalString(bal, max(t.Decimals, 6)),
				TimestampMs: nowMs,
			},
			mint: t.Mint,
			bal:  bal,
		})
	}

	// Batch-fetch USD prices for every mint we hold. A failure of the
	// Jupiter call itself is non-fatal: we just leave OraclePrice/UsdValue
	// nil for everything (matches "unknown" semantics in the snapshot
	// syncer's pair guard) so the rest of the sync proceeds.
	mints := make([]string, 0, len(pending))
	seen := make(map[string]struct{}, len(pending))
	for _, p := range pending {
		if _, ok := seen[p.mint]; ok {
			continue
		}
		seen[p.mint] = struct{}{}
		mints = append(mints, p.mint)
	}

	prices, err := c.fetchJupiterPrices(ctx, mints)
	if err != nil {
		log.Printf("solana_dex/pricing: Jupiter price fetch failed; emitting balances without USD value | wallet=%s err=%v", walletAddr, err)
		prices = map[string]float64{}
	}

	balances := make([]*models.BalanceSnapshot, 0, len(pending))
	for _, p := range pending {
		price, ok := prices[p.mint]
		if !ok || price <= 0 {
			// Crash-loud-but-soft: log a warning so the absence is
			// visible, then emit the snapshot without pricing. The
			// balance_snapshot_syncer pair guard requires both fields
			// be nil together, which is the case here.
			if p.bal.Sign() != 0 {
				log.Printf("solana_dex/pricing: no Jupiter price for held mint — leaving USD value nil | wallet=%s asset=%s mint=%s", walletAddr, p.snap.Asset, p.mint)
			}
			balances = append(balances, p.snap)
			continue
		}
		priceRat := new(big.Rat).SetFloat64(price)
		if priceRat == nil {
			balances = append(balances, p.snap)
			continue
		}
		usd := new(big.Rat).Mul(p.bal, priceRat)
		priceStr := ratToDecimalString(priceRat, 8)
		usdStr := ratToDecimalString(usd, 6)
		p.snap.OraclePrice = &priceStr
		p.snap.UsdValue = &usdStr
		balances = append(balances, p.snap)
	}

	return balances, nil
}

// fetchJupiterPrices returns a (mint → usdPrice) map for the given mints,
// hitting Jupiter's free public price API in a single batched GET (with a
// 5-minute in-process cache). Mints absent from the response or priced at
// zero are omitted from the returned map — callers must treat absence as
// "unknown" rather than "free".
//
// Errors (network, non-2xx) are returned to the caller, which logs and
// continues without USD values rather than failing the entire sync.
func (c *Client) fetchJupiterPrices(ctx context.Context, mints []string) (map[string]float64, error) {
	out := make(map[string]float64, len(mints))
	if len(mints) == 0 {
		return out, nil
	}

	// Cache hit pass.
	now := time.Now()
	c.priceCacheMu.Lock()
	if c.priceCache == nil {
		c.priceCache = make(map[string]cachedPrice)
	}
	missing := make([]string, 0, len(mints))
	for _, m := range mints {
		if cp, ok := c.priceCache[m]; ok && now.Sub(cp.cachedAt) < jupiterPriceCacheTTL {
			if cp.price > 0 {
				out[m] = cp.price
			}
			continue
		}
		missing = append(missing, m)
	}
	c.priceCacheMu.Unlock()

	if len(missing) == 0 {
		return out, nil
	}

	endpoint := c.jupiterPriceURL
	if endpoint == "" {
		endpoint = defaultJupiterPriceURL
	}
	q := url.Values{}
	q.Set("ids", strings.Join(missing, ","))
	reqURL := endpoint + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return out, fmt.Errorf("solana_dex/jupiter: build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("solana_dex/jupiter: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, fmt.Errorf("solana_dex/jupiter: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed map[string]jupiterPriceEntry
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return out, fmt.Errorf("solana_dex/jupiter: decode: %w", err)
	}

	c.priceCacheMu.Lock()
	for mint, entry := range parsed {
		if entry.UsdPrice <= 0 {
			continue
		}
		out[mint] = entry.UsdPrice
		c.priceCache[mint] = cachedPrice{price: entry.UsdPrice, cachedAt: now}
	}
	c.priceCacheMu.Unlock()

	return out, nil
}

// FetchAccountName returns "" (Solana wallets don't have an exchange-assigned
// display name). The label remains user-set.
func (c *Client) FetchAccountName(
	ctx context.Context,
	account *models.ExchangeAccount,
) (string, error) {
	return "", nil
}

// DiscoverAccounts returns the wallet itself as a single discoverable
// account. The wallet address IS the account_identifier for solana_dex.
func (c *Client) DiscoverAccounts(
	ctx context.Context,
	userIdentifier string,
) ([]*models.DiscoverableAccount, error) {
	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier (Solana wallet address) is required")
	}
	return []*models.DiscoverableAccount{
		{
			AccountIdentifier: userIdentifier,
			AccountType:       "main",
			Name:              "Solana Wallet",
			Metadata: map[string]interface{}{
				"wallet_address": userIdentifier,
			},
		},
	}, nil
}

// --- helpers below ---

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func ratAbsCmp(a, b *big.Rat) int {
	aa := new(big.Rat).Abs(new(big.Rat).Set(a))
	bb := new(big.Rat).Abs(new(big.Rat).Set(b))
	return aa.Cmp(bb)
}

// ratToDecimalString renders a big.Rat to a decimal string with up to `prec`
// digits past the decimal point. Trailing zeros and the lone dot are trimmed.
func ratToDecimalString(r *big.Rat, prec int) string {
	if r.Sign() == 0 {
		return "0"
	}
	// big.Rat.FloatString rounds half-away-from-zero, which is the standard
	// behaviour for fixed-precision decimal output.
	s := r.FloatString(prec)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func lamportsToSolDecimal(lamports int64) string {
	if lamports == 0 {
		return "0"
	}
	r := new(big.Rat).SetFrac(big.NewInt(lamports), big.NewInt(1_000_000_000))
	return ratToDecimalString(r, 9)
}

// float64ToDecimal renders the lossy-but-good-enough tokenAmount float to a
// decimal string. Helius's tokenTransfers field uses float64 even though the
// canonical source is rawTokenAmount (string + decimals). For aggregate
// per-mint deltas we use the raw path (walletNetDeltas); for individual leg
// rendering this float is fine for human-readable amounts.
func float64ToDecimal(v float64) string {
	if v == 0 {
		return "0"
	}
	// Use %.18f then trim, matching the model's convertToString convention.
	s := fmt.Sprintf("%.18f", v)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Compile-time interface check.
var _ iface.ExchangeClient = (*Client)(nil)
