package iface

import (
	"context"
	"time"

	"github.com/zif-terminal/lib/models"
)

// ExchangeClient is the interface that all exchange implementations must satisfy
type ExchangeClient interface {
	// Name returns the exchange identifier (e.g., "drift")
	Name() string

	// FetchTrades fetches trades for a given account since a specific timestamp
	// Returns trades as TradeInput (ready for database insertion), sorted by timestamp (oldest first)
	// Also returns oracle PriceRecords observed at trade/swap time (may be nil if exchange has no oracle data)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchTrades(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.TradeInput, []*models.PriceRecord, error)

	// FetchFundingPayments fetches funding payments for a given account since a specific timestamp
	// Returns funding payments as TransferInput with type="funding", sorted by timestamp (oldest first)
	// Market and payment_id are stored in metadata.
	// Filters payments where timestamp >= since (if since is not zero)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchFundingPayments(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.TransferInput, error)

	// DiscoverAccounts discovers all syncable accounts (main, subaccounts, vaults) for a given user identifier
	// For Drift: userIdentifier is the Solana wallet address (authority)
	// Returns a list of discoverable accounts that can be added for syncing
	DiscoverAccounts(
		ctx context.Context,
		userIdentifier string,
	) ([]*models.DiscoverableAccount, error)

	// FetchDeposits fetches deposits and withdrawals for a given account since a specific timestamp
	// Returns transfers as TransferInput (ready for database insertion), sorted by timestamp (oldest first)
	// Also returns oracle PriceRecords observed at deposit/withdraw time (may be nil if exchange has no oracle data)
	// ctx can be cancelled or have a timeout set by the caller (sync service)
	FetchDeposits(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.TransferInput, []*models.PriceRecord, error)

	// FetchBalances fetches current spot balances for a given account
	FetchBalances(
		ctx context.Context,
		account *models.ExchangeAccount,
	) ([]*models.BalanceSnapshot, error)

	// FetchHistoricalBalanceSnapshots fetches historical balance snapshots for backfill.
	// Returns snapshots sorted by timestamp ascending (oldest first).
	// Exchanges that don't support historical snapshots should return nil, nil.
	FetchHistoricalBalanceSnapshots(
		ctx context.Context,
		account *models.ExchangeAccount,
	) ([]*models.HistoricalBalanceSnapshots, error)

	// FetchSettlements fetches PnL settlement events for a given account since a specific timestamp.
	// On Drift, this returns actual settlement records from keeper bots that move
	// PnL + accrued funding into the spot balance.
	FetchSettlements(
		ctx context.Context,
		account *models.ExchangeAccount,
		since time.Time,
	) ([]*models.Settlement, error)

	// FetchAccountName fetches the exchange-assigned display name for an account.
	// Returns ("", nil) when the exchange does not surface a name for the account
	// (e.g., master/main accounts, exchanges without a naming API, account not found).
	// Only intended for one-shot discovery-time population of account labels; callers
	// should NOT use this for periodic refreshes — names may be user-edited downstream.
	FetchAccountName(
		ctx context.Context,
		account *models.ExchangeAccount,
	) (string, error)
}
