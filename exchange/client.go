package exchange

import (
	"errors"
	"fmt"

	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/exchange/drift"
	"github.com/zif-terminal/lib/exchange/hyperliquid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/exchange/lighter"
	"github.com/zif-terminal/lib/exchange/solana_dex"
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
	case "solana_dex":
		// solana_dex works without a DBClient (Drift sibling-lookup is a
		// defence-in-depth check; the program-id rule already covers
		// Drift txs). Callers that have a DBClient should prefer
		// GetClientWithDB to enable the sibling check.
		return solana_dex.NewClient(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrExchangeNotFound, name)
	}
}

// GetClientWithDB returns an ExchangeClient for the given exchange name,
// using the provided DB client for exchanges that read from staging tables
// (e.g., variational reads from omni_raw_events) or look up sibling
// accounts (e.g., solana_dex resolves Drift sub-account addresses for
// dedup).
// Falls back to GetClient for exchanges that don't need a DB client.
//
// As of Phase A (manual_adjustments mechanism), hyperliquid also takes a
// DB client so it can read manual_adjustments rows at the start of each
// sync cycle and merge them with real-data fetches before transformers run.
// Solana / Lighter / Drift clients are intentionally NOT wired through this
// path — Phase A scope is HL-only.
func GetClientWithDB(name string, dbClient db.DBClient) (iface.ExchangeClient, error) {
	switch name {
	case "variational":
		return variational.NewClient(dbClient), nil
	case "solana_dex":
		return solana_dex.NewClientWithDB(dbClient), nil
	case "hyperliquid":
		return hyperliquid.NewClientWithDB(dbClient), nil
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
		"solana_dex",
		"variational",
	}
}
