package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/zif-terminal/lib/models"
)

// OverviewSnapshot represents a daily Drift account snapshot with trading metrics
type OverviewSnapshot struct {
	Date  time.Time
	Earn  *EarnSnapshot
	Trade *TradeSnapshot
}

// EarnSnapshot holds per-asset earn data from a snapshot
type EarnSnapshot struct {
	Assets []EarnAssetSnapshot
}

// EarnAssetSnapshot holds per-asset balance and interest from earn snapshots
type EarnAssetSnapshot struct {
	Symbol   string
	Balance  string
	Interest string
}

// TradeSnapshot holds cumulative trading metrics from a snapshot
type TradeSnapshot struct {
	CumulativeFunding     string
	CumulativeFeePaid     string
	CumulativeRealizedPnl string
}

// PerpPosition represents a live perp position from Drift API
type PerpPosition struct {
	Symbol          string
	BaseAssetAmount *big.Float
}

// SpotBalance represents a live spot balance from Drift API
type SpotBalance struct {
	Symbol  string
	Balance *big.Float
}

// AccountState represents live account state (positions + balances)
type AccountState struct {
	Positions []PerpPosition
	Balances  []SpotBalance
}

// FetchOverviewSnapshots fetches historical daily earn snapshots for an account.
// Returns up to `limit` snapshots sorted by date ascending (0 = no limit).
func (c *Client) FetchOverviewSnapshots(ctx context.Context, account *models.ExchangeAccount, limit int) ([]*OverviewSnapshot, error) {
	wallet := c.getWalletFromAccount(account)
	if wallet == "" {
		return []*OverviewSnapshot{}, nil
	}

	// Build market-index → symbol map from the live user account
	indexToSymbol := make(map[int]string)
	if account.AccountIdentifier != "" {
		userData, err := c.fetchUserAccount(ctx, account.AccountIdentifier)
		if err == nil && userData != nil {
			for _, b := range userData.Balances {
				if b.Symbol != "" {
					indexToSymbol[b.MarketIndex] = b.Symbol
				}
			}
		}
	}

	// Fetch all historical earn snapshots
	url := fmt.Sprintf("%s/authority/%s/snapshots/earn", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch earn snapshots: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return []*OverviewSnapshot{}, nil
	}

	var result driftEarnResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []*OverviewSnapshot{}, nil
	}

	// Merge snapshots across subaccounts, keyed by epochTs
	type mergedEarn struct {
		date   time.Time
		assets map[string]float64 // symbol → balance
	}
	merged := make(map[int64]*mergedEarn)

	for _, acct := range result.Accounts {
		for _, snap := range acct.Snapshots {
			if snap.EpochTs == 0 {
				continue
			}

			m, exists := merged[snap.EpochTs]
			if !exists {
				m = &mergedEarn{
					date:   time.Unix(snap.EpochTs, 0).UTC(),
					assets: make(map[string]float64),
				}
				merged[snap.EpochTs] = m
			}

			for _, asset := range snap.Assets {
				symbol := asset.Symbol
				if symbol == "" {
					// Fall back to market-index mapping from live account
					if s, ok := indexToSymbol[asset.MarketIndex]; ok {
						symbol = s
					} else {
						continue
					}
				}
				balance := toFloat(asset.Balance)
				m.assets[symbol] += balance
			}
		}
	}

	// Convert to OverviewSnapshot slice
	snapshots := make([]*OverviewSnapshot, 0, len(merged))
	for _, m := range merged {
		earnAssets := make([]EarnAssetSnapshot, 0, len(m.assets))
		for symbol, balance := range m.assets {
			earnAssets = append(earnAssets, EarnAssetSnapshot{
				Symbol:  symbol,
				Balance: fmt.Sprintf("%.18f", balance),
			})
		}
		// Sort assets for consistent output
		sort.Slice(earnAssets, func(i, j int) bool {
			return earnAssets[i].Symbol < earnAssets[j].Symbol
		})
		snapshots = append(snapshots, &OverviewSnapshot{
			Date: m.date,
			Earn: &EarnSnapshot{Assets: earnAssets},
			// Trade snapshot not available from earn endpoint
		})
	}

	// Sort by date ascending
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Date.Before(snapshots[j].Date)
	})

	// Apply limit (take the most recent N)
	if limit > 0 && len(snapshots) > limit {
		snapshots = snapshots[len(snapshots)-limit:]
	}

	return snapshots, nil
}

// FetchAccountState fetches live positions and balances from the Drift API
func (c *Client) FetchAccountState(ctx context.Context, account *models.ExchangeAccount) (*AccountState, error) {
	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier is required")
	}

	userData, err := c.fetchUserAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user account: %w", err)
	}
	if userData == nil {
		return &AccountState{}, nil
	}

	state := &AccountState{}

	// Parse perp positions
	// Note: Drift /user/{id} API returns already-normalized values (not raw precision)
	for _, p := range userData.Positions {
		baseAmount := toFloat(p.BaseAssetAmount)
		if baseAmount == 0 {
			continue
		}

		state.Positions = append(state.Positions, PerpPosition{
			Symbol:          p.Symbol,
			BaseAssetAmount: new(big.Float).SetFloat64(baseAmount),
		})
	}

	// Parse spot balances
	// Note: Drift /user/{id} API returns already-normalized values
	for _, b := range userData.Balances {
		balance := toFloat(b.Balance)
		if balance == 0 {
			continue
		}

		state.Balances = append(state.Balances, SpotBalance{
			Symbol:  b.Symbol,
			Balance: new(big.Float).SetFloat64(balance),
		})
	}

	// Sort for consistent output
	sort.Slice(state.Positions, func(i, j int) bool {
		return state.Positions[i].Symbol < state.Positions[j].Symbol
	})
	sort.Slice(state.Balances, func(i, j int) bool {
		return state.Balances[i].Symbol < state.Balances[j].Symbol
	})

	return state, nil
}
