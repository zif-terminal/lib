package exchange

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
}
