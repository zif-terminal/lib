package iface

import (
	"context"
	"time"

	"github.com/zif-terminal/lib/models"
)

// ExchangeClient is the interface that all exchange implementations must satisfy
type ExchangeClient interface {
	// Name returns the exchange identifier (e.g., "hyperliquid", "lighter")
	Name() string

	// FetchTrades fetches trades for a given account since a specific timestamp
	// Returns trades as TradeInput (ready for database insertion), sorted by timestamp (oldest first)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchTrades(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.TradeInput, error)

	// FetchFundingPayments fetches funding payments for a given account since a specific timestamp
	// Returns funding payments as FundingPaymentInput (ready for database insertion), sorted by timestamp (oldest first)
	// Filters payments where timestamp >= since (if since is not zero)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchFundingPayments(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.FundingPaymentInput, error)
}
