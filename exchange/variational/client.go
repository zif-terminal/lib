package variational

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// Client implements iface.ExchangeClient for Variational (OMNI).
// It reads from the omni_raw_events staging table rather than calling a live API.
type Client struct {
	dbClient db.DBClient
}

// NewClient creates a new Variational exchange client.
func NewClient(dbClient db.DBClient) *Client {
	return &Client{dbClient: dbClient}
}

// Name returns the exchange identifier.
func (c *Client) Name() string {
	return "variational"
}

// FetchTrades reads trade events from omni_raw_events and converts them to TradeInput.
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, []*models.PriceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	accountID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, err
	}

	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}

	events, err := c.dbClient.ListOmniRawEvents(ctx, accountID, "trade", sinceMs)
	if err != nil {
		return nil, nil, err
	}

	trades := make([]*models.TradeInput, 0, len(events))
	for _, ev := range events {
		underlying := derefString(ev.Underlying)
		side := derefString(ev.Side)
		price := derefString(ev.Price)
		qty := derefString(ev.Qty)

		if underlying == "" {
			return nil, nil, fmt.Errorf("variational: trade %s missing required field underlying", ev.OmniID)
		}
		if side == "" {
			return nil, nil, fmt.Errorf("variational: trade %s missing required field side", ev.OmniID)
		}
		if price == "" {
			return nil, nil, fmt.Errorf("variational: trade %s missing required field price", ev.OmniID)
		}
		if qty == "" {
			return nil, nil, fmt.Errorf("variational: trade %s missing required field qty", ev.OmniID)
		}

		trade := &models.TradeInput{
			TradeID:           ev.OmniID,
			OrderID:           ev.OmniID, // OMNI doesn't have separate order IDs
			BaseAsset:         underlying,
			QuoteAsset:        "USDC",
			Side:              side,
			Price:             price,
			Quantity:          qty,
			Fee:               "0",
			FeeAsset:          "USDC",
			MarketType:        "perp",
			ExchangeAccountID: accountID,
			Timestamp:         time.Unix(0, ev.TimestampMs*int64(time.Millisecond)).UTC(),
		}
		trades = append(trades, trade)
	}

	return trades, nil, nil
}

// FetchDeposits reads deposit, withdrawal, and fee events from omni_raw_events.
func (c *Client) FetchDeposits(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, []*models.PriceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	accountID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, nil, err
	}

	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}

	events, err := c.dbClient.ListOmniRawEventsByTypes(ctx, accountID, []string{"deposit", "withdrawal", "fee"}, sinceMs)
	if err != nil {
		return nil, nil, err
	}

	transfers := make([]*models.TransferInput, 0, len(events))
	for _, ev := range events {
		var transferType string
		var amount string
		asset := "USDC"
		if ev.Asset != nil {
			asset = *ev.Asset
		}

		qtyStr := derefString(ev.Qty)

		switch ev.EventType {
		case "deposit":
			transferType = models.TypeDeposit
			amount = qtyStr
		case "withdrawal":
			transferType = models.TypeWithdraw
			amount = absNumericString(qtyStr)
		case "fee":
			transferType = models.TypeFee
			amount = absNumericString(qtyStr)
		default:
			return nil, nil, fmt.Errorf("variational: unknown event type %q for event %s", ev.EventType, ev.OmniID)
		}

		transfer := &models.TransferInput{
			ExchangeAccountID: accountID,
			Type:              transferType,
			Asset:             asset,
			Amount:            amount,
			Timestamp:         time.Unix(0, ev.TimestampMs*int64(time.Millisecond)).UTC(),
			ExternalID:        ev.OmniID,
		}

		// Add metadata for fee rows
		if ev.EventType == "fee" && ev.FeeType != nil {
			transfer.Metadata = map[string]string{
				"fee_type":   *ev.FeeType,
				"payment_id": ev.OmniID,
			}
		} else {
			transfer.Metadata = map[string]string{
				"payment_id": ev.OmniID,
			}
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil, nil
}

// FetchFundingPayments reads funding events from omni_raw_events.
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TransferInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	accountID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, err
	}

	var sinceMs int64
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}

	events, err := c.dbClient.ListOmniRawEvents(ctx, accountID, "funding", sinceMs)
	if err != nil {
		return nil, err
	}

	transfers := make([]*models.TransferInput, 0, len(events))
	for _, ev := range events {
		underlying := derefString(ev.Underlying)
		market := underlying + "-PERP"

		metadata := map[string]string{
			"market":     market,
			"payment_id": ev.OmniID,
		}
		if ev.FundingRate != nil {
			metadata["funding_rate"] = *ev.FundingRate
		}

		transfer := &models.TransferInput{
			ExchangeAccountID: accountID,
			Type:              models.TypeFunding,
			Asset:             "USDC",
			Amount:            derefString(ev.Qty),
			Timestamp:         time.Unix(0, ev.TimestampMs*int64(time.Millisecond)).UTC(),
			ExternalID:        ev.OmniID,
			Metadata:          metadata,
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

// FetchSettlements returns nil — Variational settles on close.
func (c *Client) FetchSettlements(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.Settlement, error) {
	return nil, nil
}

// FetchBalances returns nil — no live API for Variational.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	return nil, nil
}

// FetchHistoricalBalanceSnapshots returns nil — no historical snapshots for Variational.
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	return nil, nil
}

// DiscoverAccounts is not supported for Variational — accounts are created manually.
func (c *Client) DiscoverAccounts(
	ctx context.Context,
	userIdentifier string,
) ([]*models.DiscoverableAccount, error) {
	return nil, iface.ErrNotImplemented
}

// FetchAccountName is a no-op for Variational — accounts are created manually
// via the OMNI integration and labels are set by the user at creation time.
func (c *Client) FetchAccountName(
	ctx context.Context,
	account *models.ExchangeAccount,
) (string, error) {
	return "", nil
}

// derefString safely dereferences a *string, returning "" if nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// absNumericString returns the absolute value of a numeric string.
// If the string starts with '-', the prefix is stripped.
func absNumericString(s string) string {
	if strings.HasPrefix(s, "-") {
		// Parse and return absolute value to handle edge cases
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return strings.TrimPrefix(s, "-")
		}
		return strconv.FormatFloat(math.Abs(f), 'f', -1, 64)
	}
	return s
}
