package exchange

import (
	"errors"
	"fmt"

	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/exchange/drift"
	"github.com/zif-terminal/lib/exchange/hyperliquid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/exchange/lighter"
	"github.com/zif-terminal/lib/exchange/variational"
)

// ErrExchangeNotFound is returned when an exchange name is not recognized
var ErrExchangeNotFound = errors.New("exchange not found")

// GetClient returns an ExchangeClient for the given exchange name.
// Returns ErrExchangeNotFound if the exchange name is not recognized.
// For exchanges that require a DB client (e.g., variational), use GetClientWithDB instead.
//
// Example:
//
//	client, err := exchange.GetClient("drift")
//	if err != nil {
//	    return err
//	}
//	trades, err := client.FetchTrades(ctx, account, since)
func GetClient(name string) (iface.ExchangeClient, error) {
	switch name {
	case "drift":
		return drift.NewClient(), nil
	case "hyperliquid":
		return hyperliquid.NewClient(), nil
	case "lighter":
		return lighter.NewClient(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrExchangeNotFound, name)
	}
}

// GetClientWithDB returns an ExchangeClient for the given exchange name,
// using the provided DB client for exchanges that read from staging tables
// (e.g., variational reads from omni_raw_events).
// Falls back to GetClient for exchanges that don't need a DB client.
func GetClientWithDB(name string, dbClient db.DBClient) (iface.ExchangeClient, error) {
	switch name {
	case "variational":
		return variational.NewClient(dbClient), nil
	default:
		return GetClient(name)
	}
}

// ListAvailableExchanges returns a list of all available exchange names.
func ListAvailableExchanges() []string {
	return []string{
		"drift",
		"hyperliquid",
		"lighter",
		"variational",
	}
}
