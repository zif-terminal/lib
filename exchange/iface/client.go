package iface

import (
	"context"
	"time"

	"github.com/zif-terminal/lib/models"
)

// ExchangeClient is the interface that all exchange implementations must satisfy
type ExchangeClient interface {
	// Name returns the exchange identifier (e.g., "hyperliquid", "drift")
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

	// DiscoverAccounts discovers all syncable accounts (main, subaccounts, vaults) for a given user identifier
	// For Hyperliquid: userIdentifier is the wallet address (0x...)
	// For Drift: userIdentifier is the Solana wallet address (authority)
	// Returns a list of discoverable accounts that can be added for syncing
	DiscoverAccounts(
		ctx context.Context,
		userIdentifier string,
	) ([]*models.DiscoverableAccount, error)

	// FetchDeposits fetches deposits and withdrawals for a given account since a specific timestamp
	// Returns deposits as DepositInput (ready for database insertion), sorted by timestamp (oldest first)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchDeposits(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.DepositInput, error)

	// FetchPositions fetches current open positions (perp + spot) for a given account
	FetchPositions(
		ctx context.Context,
		account *models.ExchangeAccount,
	) ([]*models.PositionSnapshot, error)

	// FetchBalances fetches current spot balances for a given account
	FetchBalances(
		ctx context.Context,
		account *models.ExchangeAccount,
	) ([]*models.BalanceSnapshot, error)

	// FetchOpenOrders fetches currently active orders for a given account
	FetchOpenOrders(
		ctx context.Context,
		account *models.ExchangeAccount,
	) ([]*models.OpenOrder, error)

	// FetchAccountValue fetches total equity/account value for a given account
	FetchAccountValue(
		ctx context.Context,
		account *models.ExchangeAccount,
	) (*models.AccountValueSnapshot, error)
}
