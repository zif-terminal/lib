package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// ExchangeAccount represents an exchange account model (aliased from models package)
type ExchangeAccount = models.ExchangeAccount

// ExchangeAccountInput represents exchange account input for mutations (aliased from models package)
type ExchangeAccountInput = models.ExchangeAccountInput

// AccountListFilter controls which accounts ListAccounts returns.
// Nil fields mean "don't filter on this flag". When both are nil, all
// accounts are returned.
//
// OrSyncResetRequested / OrProcessorResetRequested widen the query with
// an _or clause so that accounts with a pending reset are also returned
// even when the corresponding enabled flag is false. Example: setting
// SyncEnabled=true + OrSyncResetRequested=true produces:
//
//	_or: [{sync_enabled: {_eq: true}}, {sync_reset_requested: {_eq: true}}]
type AccountListFilter struct {
	SyncEnabled       *bool
	ProcessingEnabled *bool

	// When true, adds an _or branch for sync_reset_requested = true.
	OrSyncResetRequested *bool
	// When true, adds an _or branch for processor_reset_requested = true.
	OrProcessorResetRequested *bool
}

// GetAccount retrieves a single exchange account by ID
func (c *Client) GetAccount(ctx context.Context, id string) (*ExchangeAccount, error) {
	query := `
		query GetAccount($id: uuid!) {
			exchange_accounts_by_pk(id: $id) {
				id
				account_identifier
				account_type
				account_type_metadata
				status
				sync_enabled
				processing_enabled
				sync_reset_requested
				processor_reset_requested
				exchange {
					id
					name
					display_name
					settles_on_close
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		ExchangeAccountsByPk *ExchangeAccount `json:"exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	if resp.ExchangeAccountsByPk == nil {
		return nil, notFoundError("account", id)
	}

	return resp.ExchangeAccountsByPk, nil
}

// ListAccounts retrieves exchange accounts matching the given filter.
// Pass AccountListFilter{} to get all accounts.
func (c *Client) ListAccounts(ctx context.Context, f AccountListFilter) ([]*ExchangeAccount, error) {
	where := map[string]interface{}{}

	// Build _or branches when a reset flag should widen the result set.
	// e.g. SyncEnabled=true + OrSyncResetRequested=true →
	//   _or: [{sync_enabled: {_eq: true}}, {sync_reset_requested: {_eq: true}}]
	var orBranches []map[string]interface{}

	if f.SyncEnabled != nil && f.OrSyncResetRequested != nil && *f.OrSyncResetRequested {
		orBranches = append(orBranches,
			map[string]interface{}{"sync_enabled": map[string]interface{}{"_eq": *f.SyncEnabled}},
			map[string]interface{}{"sync_reset_requested": map[string]interface{}{"_eq": true}},
		)
	} else if f.SyncEnabled != nil {
		where["sync_enabled"] = map[string]interface{}{"_eq": *f.SyncEnabled}
	} else if f.OrSyncResetRequested != nil && *f.OrSyncResetRequested {
		where["sync_reset_requested"] = map[string]interface{}{"_eq": true}
	}

	if f.ProcessingEnabled != nil && f.OrProcessorResetRequested != nil && *f.OrProcessorResetRequested {
		orBranches = append(orBranches,
			map[string]interface{}{"processing_enabled": map[string]interface{}{"_eq": *f.ProcessingEnabled}},
			map[string]interface{}{"processor_reset_requested": map[string]interface{}{"_eq": true}},
		)
	} else if f.ProcessingEnabled != nil {
		where["processing_enabled"] = map[string]interface{}{"_eq": *f.ProcessingEnabled}
	} else if f.OrProcessorResetRequested != nil && *f.OrProcessorResetRequested {
		where["processor_reset_requested"] = map[string]interface{}{"_eq": true}
	}

	if len(orBranches) > 0 {
		where["_or"] = orBranches
	}

	query := `
		query ListAccounts($where: exchange_accounts_bool_exp!) {
			exchange_accounts(where: $where) {
				id
				account_identifier
				account_type
				account_type_metadata
				status
				sync_enabled
				processing_enabled
				sync_reset_requested
				processor_reset_requested
				exchange {
					id
					name
					display_name
					settles_on_close
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"where": where,
	})

	var resp struct {
		ExchangeAccounts []*ExchangeAccount `json:"exchange_accounts"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	return resp.ExchangeAccounts, nil
}

// CreateAccount creates a new exchange account
func (c *Client) CreateAccount(ctx context.Context, input *ExchangeAccountInput) (*ExchangeAccount, error) {
	query := `
		mutation CreateAccount($exchange_id: uuid!, $account_identifier: String!, $account_type: String!, $account_type_metadata: jsonb, $wallet_id: uuid, $status: String, $label: String) {
			insert_exchange_accounts_one(object: {
				exchange_id: $exchange_id
				account_identifier: $account_identifier
				account_type: $account_type
				account_type_metadata: $account_type_metadata
				wallet_id: $wallet_id
				status: $status
				label: $label
			}) {
				id
				account_identifier
				account_type
				account_type_metadata
				wallet_id
				status
				label
				exchange {
					id
					name
					display_name
				}
			}
		}
	`

	vars := map[string]interface{}{
		"exchange_id":        input.ExchangeID,
		"account_identifier": input.AccountIdentifier,
		"account_type":       input.AccountType,
	}

	// Only include metadata if it's not empty
	if len(input.AccountTypeMetadata) > 0 {
		var metadata interface{}
		if err := json.Unmarshal(input.AccountTypeMetadata, &metadata); err == nil {
			vars["account_type_metadata"] = metadata
		}
	}

	// Include wallet_id if provided
	if input.WalletID != nil {
		vars["wallet_id"] = *input.WalletID
	}

	// Include status if provided
	if input.Status != "" {
		vars["status"] = input.Status
	}

	// Include label if provided (non-nil, non-empty)
	if input.Label != nil && *input.Label != "" {
		vars["label"] = *input.Label
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertExchangeAccountsOne *ExchangeAccount `json:"insert_exchange_accounts_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create account: %w", err)
	}

	if resp.InsertExchangeAccountsOne == nil {
		return nil, fmt.Errorf("failed to create account: no data returned")
	}

	return resp.InsertExchangeAccountsOne, nil
}

// UpdateAccount updates an existing exchange account
func (c *Client) UpdateAccount(ctx context.Context, id string, input *ExchangeAccountInput) (*ExchangeAccount, error) {
	query := `
		mutation UpdateAccount($id: uuid!, $exchange_id: uuid!, $account_identifier: String!, $account_type: String!, $account_type_metadata: jsonb) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {
				exchange_id: $exchange_id
				account_identifier: $account_identifier
				account_type: $account_type
				account_type_metadata: $account_type_metadata
			}) {
				id
				account_identifier
				account_type
				account_type_metadata
				exchange {
					id
					name
					display_name
				}
			}
		}
	`

	vars := map[string]interface{}{
		"id":                id,
		"exchange_id":       input.ExchangeID,
		"account_identifier": input.AccountIdentifier,
		"account_type":       input.AccountType,
	}

	// Only include metadata if it's not empty
	if len(input.AccountTypeMetadata) > 0 {
		var metadata interface{}
		if err := json.Unmarshal(input.AccountTypeMetadata, &metadata); err == nil {
			vars["account_type_metadata"] = metadata
		}
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		UpdateExchangeAccountsByPk *ExchangeAccount `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to update account: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return nil, notFoundError("account", id)
	}

	return resp.UpdateExchangeAccountsByPk, nil
}

// DeleteAccount deletes an exchange account by ID
func (c *Client) DeleteAccount(ctx context.Context, id string) error {
	query := `
		mutation DeleteAccount($id: uuid!) {
			delete_exchange_accounts_by_pk(id: $id) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		DeleteExchangeAccountsByPk struct {
			ID string `json:"id"`
		} `json:"delete_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}

	if resp.DeleteExchangeAccountsByPk.ID == "" {
		return notFoundError("account", id)
	}

	return nil
}

// ListAccountTypes retrieves all available account types
func (c *Client) ListAccountTypes(ctx context.Context) ([]*models.AccountType, error) {
	query := `
		query ListAccountTypes {
			exchange_account_types {
				code
			}
		}
	`

	req := c.graphqlRequest(query)

	var resp struct {
		AccountTypes []*models.AccountType `json:"exchange_account_types"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list account types: %w", err)
	}

	return resp.AccountTypes, nil
}

// UpdateAccountTags updates the tags for an account
func (c *Client) UpdateAccountTags(ctx context.Context, accountID string, tags []string) error {
	query := `
		mutation UpdateAccountTags($id: uuid!, $tags: jsonb!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {tags: $tags}) {
				id
				tags
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":   accountID,
		"tags": tags,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *ExchangeAccount `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update account tags: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// UpdateAccountLabel sets the label column on an exchange account.
//
// Intended for one-shot discovery-time population of labels, e.g. from
// FetchAccountName. The caller is responsible for the "only fill when empty"
// guard — this method unconditionally overwrites whatever is there, so
// callers must check label == nil / "" first to avoid clobbering user-set
// values.
//
// Updates ONLY the label column; all other columns are untouched.
func (c *Client) UpdateAccountLabel(ctx context.Context, accountID string, label string) error {
	query := `
		mutation UpdateAccountLabel($id: uuid!, $label: String!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {label: $label}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":    accountID,
		"label": label,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update account label: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// UpdateAccountLastSynced updates the last_synced_at timestamp for an account
func (c *Client) UpdateAccountLastSynced(ctx context.Context, accountID string) error {
	query := `
		mutation UpdateAccountLastSynced($id: uuid!, $last_synced_at: timestamptz!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {last_synced_at: $last_synced_at}) {
				id
				last_synced_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":             accountID,
		"last_synced_at": "now()",
	})

	var resp struct {
		UpdateExchangeAccountsByPk *ExchangeAccount `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update account last_synced_at: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// SetAccountSyncError records a sync error message on the exchange account.
func (c *Client) SetAccountSyncError(ctx context.Context, accountID string, errorMsg string) error {
	query := `
		mutation SetAccountSyncError($id: uuid!, $last_sync_error: String!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {last_sync_error: $last_sync_error}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":              accountID,
		"last_sync_error": errorMsg,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to set account sync error: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// ClearAccountSyncError clears any sync error on the exchange account.
func (c *Client) ClearAccountSyncError(ctx context.Context, accountID string) error {
	query := `
		mutation ClearAccountSyncError($id: uuid!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {last_sync_error: null}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to clear account sync error: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// ClearSyncReset clears the sync reset flag and resets sync state on an account.
func (c *Client) ClearSyncReset(ctx context.Context, accountID string) error {
	query := `
		mutation ClearSyncReset($id: uuid!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {
				sync_reset_requested: false
				last_synced_at: null
				last_sync_error: null
			}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to clear sync reset: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// ClearProcessorReset clears the processor reset flag on an account.
func (c *Client) ClearProcessorReset(ctx context.Context, accountID string) error {
	query := `
		mutation ClearProcessorReset($id: uuid!) {
			update_exchange_accounts_by_pk(pk_columns: {id: $id}, _set: {
				processor_reset_requested: false
			}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID,
	})

	var resp struct {
		UpdateExchangeAccountsByPk *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to clear processor reset: %w", err)
	}

	if resp.UpdateExchangeAccountsByPk == nil {
		return notFoundError("account", accountID)
	}

	return nil
}

// DeleteSyncedData deletes all synced data for an account: trades, transfers,
// settlements, and spot balance snapshots.
func (c *Client) DeleteSyncedData(ctx context.Context, accountID uuid.UUID) error {
	id := accountID.String()

	// Delete trades
	tradesQuery := `
		mutation DeleteTradesForAccount($id: uuid!) {
			delete_trades(where: { exchange_account_id: { _eq: $id } }) {
				affected_rows
			}
		}
	`
	req := c.graphqlRequestWithVars(tradesQuery, map[string]interface{}{"id": id})
	var tradesResp struct {
		DeleteTrades struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_trades"`
	}
	if err := c.execute(ctx, req, &tradesResp); err != nil {
		return fmt.Errorf("failed to delete trades: %w", err)
	}

	// Delete transfers
	transfersQuery := `
		mutation DeleteTransfersForAccount($id: uuid!) {
			delete_transfers(where: { exchange_account_id: { _eq: $id } }) {
				affected_rows
			}
		}
	`
	req = c.graphqlRequestWithVars(transfersQuery, map[string]interface{}{"id": id})
	var transfersResp struct {
		DeleteTransfers struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_transfers"`
	}
	if err := c.execute(ctx, req, &transfersResp); err != nil {
		return fmt.Errorf("failed to delete transfers: %w", err)
	}

	// Delete settlements
	settlementsQuery := `
		mutation DeleteSettlementsForAccount($id: uuid!) {
			delete_settlements(where: { exchange_account_id: { _eq: $id } }) {
				affected_rows
			}
		}
	`
	req = c.graphqlRequestWithVars(settlementsQuery, map[string]interface{}{"id": id})
	var settlementsResp struct {
		DeleteSettlements struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_settlements"`
	}
	if err := c.execute(ctx, req, &settlementsResp); err != nil {
		return fmt.Errorf("failed to delete settlements: %w", err)
	}

	// Delete spot balance snapshots
	snapshotsQuery := `
		mutation DeleteSnapshotsForAccount($id: uuid!) {
			delete_spot_balance_snapshots(where: { exchange_account_id: { _eq: $id } }) {
				affected_rows
			}
		}
	`
	req = c.graphqlRequestWithVars(snapshotsQuery, map[string]interface{}{"id": id})
	var snapshotsResp struct {
		DeleteSpotBalanceSnapshots struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_spot_balance_snapshots"`
	}
	if err := c.execute(ctx, req, &snapshotsResp); err != nil {
		return fmt.Errorf("failed to delete spot balance snapshots: %w", err)
	}

	return nil
}

// DeleteProcessedData deletes all processed data for an account: positions
// (cascades to position_events and position_pnl), processor checkpoints,
// and derived transfers.
func (c *Client) DeleteProcessedData(ctx context.Context, accountID uuid.UUID) error {
	// Delete positions (cascades to position_events and position_pnl via DB FK)
	if _, err := c.DeletePositionsForAccount(ctx, accountID); err != nil {
		return fmt.Errorf("failed to delete positions: %w", err)
	}

	// Delete processor checkpoint
	if err := c.DeleteCheckpoint(ctx, accountID); err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	// Delete derived transfers
	if _, err := c.DeleteDerivedTransfers(ctx, accountID); err != nil {
		return fmt.Errorf("failed to delete derived transfers: %w", err)
	}

	return nil
}
