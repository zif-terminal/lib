package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Transfer aliased from models
type Transfer = models.Transfer

// TransferFilter aliased from models
type TransferFilter = models.TransferFilter

// TransferInput aliased from models
type TransferInput = models.TransferInput

// ListTransfers queries the transfers table with optional account ID filter.
func (c *Client) ListTransfers(ctx context.Context, filter TransferFilter) ([]*Transfer, error) {
	where := map[string]interface{}{}
	if len(filter.ExchangeAccountIDs) > 0 {
		ids := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			ids[i] = id.String()
		}
		where["exchange_account_id"] = map[string]interface{}{
			"_in": ids,
		}
	}

	query := `
		query ListTransfers($where: transfers_bool_exp!) {
			transfers(where: $where, order_by: { timestamp: asc }) {
				id
				exchange_account_id
				type
				asset
				amount
				timestamp
				metadata
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"where": where,
	})

	var resp struct {
		Transfers []*Transfer `json:"transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list transfers: %w", err)
	}

	return resp.Transfers, nil
}

// GetTransfersByIDs retrieves transfers by their IDs.
func (c *Client) GetTransfersByIDs(ctx context.Context, ids []uuid.UUID) ([]*Transfer, error) {
	if len(ids) == 0 {
		return []*Transfer{}, nil
	}

	query := `
		query GetTransfersByIDs($ids: [uuid!]!) {
			transfers(where: { id: { _in: $ids } }) {
				id
				exchange_account_id
				type
				asset
				amount
				timestamp
				metadata
			}
		}
	`

	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"ids": idStrings,
	})

	var resp struct {
		Transfers []*Transfer `json:"transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get transfers by IDs: %w", err)
	}

	return resp.Transfers, nil
}

// AddTransfers inserts transfer records into the transfers table.
// Duplicates raise a unique-constraint violation and must be handled by callers —
// silent ON CONFLICT tolerance is a bug magnet at the re-sync cursor boundary.
//
// Before inserting, upgrades any existing rows that have external_id = '' to
// the real external_id when the base columns (exchange_account_id, type, asset,
// timestamp) match. This prevents duplicates when the same transfer was
// originally synced without an external_id and later re-synced with one.
func (c *Client) AddTransfers(ctx context.Context, inputs []*TransferInput) ([]*Transfer, error) {
	if len(inputs) == 0 {
		return []*Transfer{}, nil
	}

	// Upgrade empty external_ids on existing rows before inserting.
	if err := c.upgradeTransferExternalIDs(ctx, inputs); err != nil {
		return nil, fmt.Errorf("failed to upgrade transfer external IDs: %w", err)
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"type":                input.Type,
			"asset":               input.Asset,
			"amount":              input.Amount,
			"timestamp":           input.Timestamp.UnixMilli(),
		}
		if input.ExternalID != "" {
			obj["external_id"] = input.ExternalID
		}
		if len(input.Metadata) > 0 {
			obj["metadata"] = input.Metadata
		}
		objects[i] = obj
	}

	query := `
		mutation AddTransfers($objects: [transfers_insert_input!]!) {
			insert_transfers(objects: $objects) {
				returning {
					id
					exchange_account_id
					type
					asset
					amount
					timestamp
					external_id
					metadata
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertTransfers struct {
			Returning []*Transfer `json:"returning"`
		} `json:"insert_transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add transfers: %w", err)
	}

	return resp.InsertTransfers.Returning, nil
}

// upgradeTransferExternalIDs updates existing transfers that have external_id = ''
// to the real external_id when all base columns match. This is done one at a time
// since each transfer has a unique external_id value. Only inputs with non-empty
// ExternalID are processed.
func (c *Client) upgradeTransferExternalIDs(ctx context.Context, inputs []*TransferInput) error {
	for _, input := range inputs {
		if input.ExternalID == "" {
			continue
		}

		query := `
			mutation UpgradeTransferExternalID(
				$account_id: uuid!,
				$type: String!,
				$asset: String!,
				$ts: bigint!,
				$external_id: String!
			) {
				update_transfers(
					where: {
						exchange_account_id: { _eq: $account_id }
						type: { _eq: $type }
						asset: { _eq: $asset }
						timestamp: { _eq: $ts }
						external_id: { _eq: "" }
					}
					_set: { external_id: $external_id }
				) {
					affected_rows
				}
			}
		`

		req := c.graphqlRequestWithVars(query, map[string]interface{}{
			"account_id":  input.ExchangeAccountID.String(),
			"type":        input.Type,
			"asset":       input.Asset,
			"ts":          input.Timestamp.UnixMilli(),
			"external_id": input.ExternalID,
		})

		var resp struct {
			UpdateTransfers struct {
				AffectedRows int `json:"affected_rows"`
			} `json:"update_transfers"`
		}

		if err := c.execute(ctx, req, &resp); err != nil {
			return fmt.Errorf("failed to upgrade external_id for transfer %s/%s/%s: %w",
				input.Type, input.Asset, input.ExternalID, err)
		}
	}

	return nil
}

// DeleteTransfersByAccountAndType deletes transfer records matching account ID and type.
func (c *Client) DeleteTransfersByAccountAndType(ctx context.Context, accountID uuid.UUID, transferType string) (int, error) {
	query := `
		mutation DeleteTransfers($exchange_account_id: uuid!, $type: String!) {
			delete_transfers(where: {
				exchange_account_id: { _eq: $exchange_account_id }
				type: { _eq: $type }
			}) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
		"type":                transferType,
	})

	var resp struct {
		DeleteTransfers struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete transfers: %w", err)
	}

	return resp.DeleteTransfers.AffectedRows, nil
}

// DeleteDerivedTransfers deletes transfer records with metadata source="derived" for an account.
// Used by the activity processor to clean up previously-derived interest before full replay.
func (c *Client) DeleteDerivedTransfers(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		mutation DeleteDerivedTransfers($exchange_account_id: uuid!) {
			delete_transfers(where: {
				exchange_account_id: { _eq: $exchange_account_id }
				metadata: { _contains: { source: "derived" } }
			}) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
	})

	var resp struct {
		DeleteTransfers struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete derived transfers: %w", err)
	}

	return resp.DeleteTransfers.AffectedRows, nil
}

// GetLatestTransferByType retrieves the most recent transfer of a given type for an account.
// Returns nil, nil if no matching transfer is found.
func (c *Client) GetLatestTransferByType(ctx context.Context, accountID uuid.UUID, transferType string) (*Transfer, error) {
	query := `
		query GetLatestTransferByType($exchange_account_id: uuid!, $type: String!) {
			transfers(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
					type: { _eq: $type }
				}
				order_by: { timestamp: desc }
				limit: 1
			) {
				id
				exchange_account_id
				type
				asset
				amount
				timestamp
				metadata
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
		"type":                transferType,
	})

	var resp struct {
		Transfers []*Transfer `json:"transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest transfer by type: %w", err)
	}

	if len(resp.Transfers) == 0 {
		return nil, nil
	}

	return resp.Transfers[0], nil
}
