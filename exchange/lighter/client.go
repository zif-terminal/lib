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

// doGet performs an authenticated GET request with rate limiting and retry on 429.
func (c *Client) doGet(ctx context.Context, url string) (json.RawMessage, error) {
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
		// 400 is a client-side error — always a bug in the request we built.
		if resp.StatusCode == http.StatusBadRequest {
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

	rawTrades, err := fetchAllPages[lighterTrade](ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/trades?account_index=%d&sort_by=timestamp&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	})
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

		// Determine side: if account is the bid account, they're buying; if ask, selling
		side := "buy"
		orderID := rt.BidOrderID
		if rt.AskAccountID == accountIndex {
			side = "sell"
			orderID = rt.AskOrderID
		}

		// Determine fee: taker vs maker
		fee := rt.MakerFee
		if (side == "buy" && rt.IsBuyerTaker) || (side == "sell" && !rt.IsBuyerTaker) {
			fee = rt.TakerFee
		}

		ts := time.UnixMilli(rt.Timestamp).UTC()

		trades = append(trades, &models.TradeInput{
			TradeID:           rt.TradeID,
			OrderID:           orderID,
			BaseAsset:         market.BaseAsset,
			QuoteAsset:        market.QuoteAsset,
			Side:              side,
			Price:             cleanDecimal(rt.Price),
			Quantity:          cleanDecimal(rt.Quantity),
			Fee:               cleanDecimal(fee),
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

	rawFunding, err := fetchAllPages[lighterFunding](ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/positionFunding?account_index=%d&limit=%d",
			c.baseURL, accountIndex, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch funding: %w", err)
	}

	sinceMs := since.UnixMilli()
	payments := make([]*models.TransferInput, 0, len(rawFunding))

	for _, rf := range rawFunding {
		if !since.IsZero() && rf.Timestamp < sinceMs {
			continue
		}

		market, err := c.resolveMarket(ctx, rf.MarketID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve market %d: %w", rf.MarketID, err)
		}

		payments = append(payments, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              models.TypeFunding,
			Asset:             market.QuoteAsset,
			Amount:            rf.Amount,
			Timestamp:         time.UnixMilli(rf.Timestamp).UTC(),
			ExternalID:        rf.FundingID,
			Metadata: map[string]string{
				"market":     market.Symbol + "-PERP",
				"payment_id": rf.FundingID,
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

	// Extract l1_address from account metadata or use wallet address
	l1Address := extractL1Address(account)

	rawDeposits, err := fetchAllPages[lighterDeposit](ctx, c, func(cursor string) string {
		url := fmt.Sprintf("%s/deposit/history?account_index=%d&l1_address=%s&filter=all&limit=%d",
			c.baseURL, accountIndex, l1Address, defaultPageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		return url
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch deposits: %w", err)
	}

	sinceMs := since.UnixMilli()
	transfers := make([]*models.TransferInput, 0, len(rawDeposits))

	for _, rd := range rawDeposits {
		if !since.IsZero() && rd.Timestamp < sinceMs {
			continue
		}

		// Determine deposit vs withdrawal from amount sign
		amount := rd.Amount
		transferType := models.TypeDeposit
		if strings.HasPrefix(amount, "-") {
			transferType = models.TypeWithdraw
			amount = amount[1:] // Make positive
		}

		// Resolve asset from asset_id (0 = USDC on Lighter)
		asset := "USDC"

		transfers = append(transfers, &models.TransferInput{
			ExchangeAccountID: accountUUID,
			Type:              transferType,
			Asset:             asset,
			Amount:            cleanDecimal(amount),
			Timestamp:         time.UnixMilli(rd.Timestamp).UTC(),
			Metadata: map[string]string{
				"deposit_id": rd.DepositID,
				"tx_hash":    rd.TxHash,
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
