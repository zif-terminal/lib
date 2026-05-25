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
//
// Returns an error (rather than an empty slice) if Hasura responds with an
// unexpectedly empty `returning` while inputs were non-empty and no upgrade
// path absorbed them — historically (incident: solana_dex 2026-05-07) such a
// silent zero-row insert can occur if a unique-constraint violation gets
// reported in `errors[]` but the response is otherwise shaped like success.
// We fail loudly here so the caller's sync cycle reports the failure rather
// than logging "deposits_fetched=N" while writing nothing.
func (c *Client) AddTransfers(ctx context.Context, inputs []*TransferInput) ([]*Transfer, error) {
	if len(inputs) == 0 {
		return []*Transfer{}, nil
	}

	// Upgrade empty external_ids on existing rows before inserting. The number
	// of rows upgraded is returned so we can validate the subsequent insert
	// count (upgrade-then-insert is supposed to net to len(inputs) writes).
	upgraded, err := c.upgradeTransferExternalIDs(ctx, inputs)
	if err != nil {
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
		// Surface the first input's identifying info so log readers can find
		// the offending row in Hasura logs / postgres.
		first := inputs[0]
		return nil, fmt.Errorf("failed to add transfers (count=%d, first=%s/%s/%s/%d/%s): %w",
			len(inputs), first.ExchangeAccountID.String(), first.Type, first.Asset,
			first.Timestamp.UnixMilli(), first.ExternalID, err)
	}

	inserted := len(resp.InsertTransfers.Returning)
	expected := len(inputs) - upgraded
	if expected < 0 {
		// upgrade affected more rows than we asked for — the upgrade query
		// matches by (account, type, asset, ts) so a single new input can
		// cover multiple legacy empty-external_id rows. Treat this as
		// inserted==0 acceptable (we cannot tell which input matched what),
		// but require at least one row was either upgraded or inserted.
		expected = 0
	}
	if inserted != expected {
		first := inputs[0]
		return nil, fmt.Errorf(
			"AddTransfers silent-write detected: inputs=%d upgraded=%d expected_insert=%d actual_insert=%d (first_input=%s/%s/%s/%d/%s) — Hasura returned success but the insert did not commit the expected rows (likely a duplicate/permission/JSON-shape issue swallowed by the GraphQL transport)",
			len(inputs), upgraded, expected, inserted,
			first.ExchangeAccountID.String(), first.Type, first.Asset,
			first.Timestamp.UnixMilli(), first.ExternalID)
	}

	return resp.InsertTransfers.Returning, nil
}

// upgradeTransferExternalIDs updates existing transfers that have external_id = ''
// to the real external_id when all base columns match. This is done one at a time
// since each transfer has a unique external_id value. Only inputs with non-empty
// ExternalID are processed. Returns the total number of rows upgraded so the
// caller can validate the subsequent insert (upgrade-then-insert should net to
// len(inputs) total writes).
func (c *Client) upgradeTransferExternalIDs(ctx context.Context, inputs []*TransferInput) (int, error) {
	upgraded := 0
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
			return upgraded, fmt.Errorf("failed to upgrade external_id for transfer %s/%s/%s: %w",
				input.Type, input.Asset, input.ExternalID, err)
		}
		upgraded += resp.UpdateTransfers.AffectedRows
	}

	return upgraded, nil
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

// UpdateTransferTimestamp updates the timestamp column on a single transfer row.
//
// Used by the post-fetch phantom-funding clamp (Fix B): when a funding payment
// or borrow/lend interest event is dated before the position it funds was
// actually opened (HL's daily-rollup bucketing can stamp funding at D 00:00 UTC
// even when the position opened at D 13:21 UTC), the clamp shifts the timestamp
// forward to the first-trade timestamp so downstream processors don't see a
// phantom short.
//
// Updates ONLY the timestamp column; all other columns (amount, asset, type,
// metadata) are untouched — the raw HL window_start_ms in metadata is preserved
// so R2 can still detect genuine multi-day funding-before-fill gaps.
func (c *Client) UpdateTransferTimestamp(ctx context.Context, transferID uuid.UUID, timestamp int64) error {
	query := `
		mutation UpdateTransferTimestamp($id: uuid!, $ts: bigint!) {
			update_transfers_by_pk(pk_columns: {id: $id}, _set: {timestamp: $ts}) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": transferID.String(),
		"ts": timestamp,
	})

	var resp struct {
		UpdateTransfersByPk *struct {
			ID string `json:"id"`
		} `json:"update_transfers_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update transfer timestamp: %w", err)
	}

	if resp.UpdateTransfersByPk == nil {
		return notFoundError("transfer", transferID.String())
	}

	return nil
}
