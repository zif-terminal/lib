package exchange

import (
	"errors"
	"fmt"

	"github.com/zif-terminal/lib/exchange/hyperliquid"
	"github.com/zif-terminal/lib/exchange/iface"
)

// ErrExchangeNotFound is returned when an exchange name is not recognized
var ErrExchangeNotFound = errors.New("exchange not found")

// GetClient returns an ExchangeClient for the given exchange name.
// Returns ErrExchangeNotFound if the exchange name is not recognized.
//
// Example:
//
//	client, err := exchange.GetClient("hyperliquid")
//	if err != nil {
//	    return err
//	}
//	trades, err := client.FetchTrades(ctx, account, since)
func GetClient(name string) (iface.ExchangeClient, error) {
	switch name {
	case "hyperliquid":
		return hyperliquid.NewClient(), nil
	// Add more exchanges here as they are implemented:
	// case "lighter":
	//     return lighter.NewClient(), nil
	// case "drift":
	//     return drift.NewClient(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrExchangeNotFound, name)
	}
}

// ListAvailableExchanges returns a list of all available exchange names.
func ListAvailableExchanges() []string {
	return []string{
		"hyperliquid",
		// Add more exchanges here as they are implemented:
		// "lighter",
		// "drift",
	}
}
