package db

import (
	"context"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// Wallet type alias from models package
type Wallet = models.Wallet
type WalletInput = models.WalletInput

// GetWallet retrieves a wallet by ID
func (c *Client) GetWallet(ctx context.Context, id string) (*Wallet, error) {
	query := `
		query GetWallet($id: uuid!) {
			wallets_by_pk(id: $id) {
				id
				address
				chain
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		WalletsByPk *Wallet `json:"wallets_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	if resp.WalletsByPk == nil {
		return nil, notFoundError("wallet", id)
	}

	return resp.WalletsByPk, nil
}

// GetWalletByAddress retrieves a wallet by address and chain
func (c *Client) GetWalletByAddress(ctx context.Context, address string, chain string) (*Wallet, error) {
	query := `
		query GetWalletByAddress($address: String!, $chain: String!) {
			wallets(where: {address: {_eq: $address}, chain: {_eq: $chain}}, limit: 1) {
				id
				address
				chain
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"address": address,
		"chain":   chain,
	})

	var resp struct {
		Wallets []*Wallet `json:"wallets"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get wallet by address: %w", err)
	}

	if len(resp.Wallets) == 0 {
		return nil, nil // Not found, return nil without error
	}

	return resp.Wallets[0], nil
}

// ListWallets retrieves all wallets
func (c *Client) ListWallets(ctx context.Context) ([]*Wallet, error) {
	query := `
		query ListWallets {
			wallets(order_by: {created_at: desc}) {
				id
				address
				chain
				created_at
			}
		}
	`

	req := c.graphqlRequest(query)

	var resp struct {
		Wallets []*Wallet `json:"wallets"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	return resp.Wallets, nil
}

// ListWalletsByChain retrieves wallets for a specific chain
func (c *Client) ListWalletsByChain(ctx context.Context, chain string) ([]*Wallet, error) {
	query := `
		query ListWalletsByChain($chain: String!) {
			wallets(where: {chain: {_eq: $chain}}, order_by: {created_at: desc}) {
				id
				address
				chain
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"chain": chain,
	})

	var resp struct {
		Wallets []*Wallet `json:"wallets"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list wallets by chain: %w", err)
	}

	return resp.Wallets, nil
}

// CreateWallet creates a new wallet (or returns existing if duplicate)
func (c *Client) CreateWallet(ctx context.Context, input *WalletInput) (*Wallet, error) {
	query := `
		mutation CreateWallet($address: String!, $chain: String!) {
			insert_wallets_one(
				object: {address: $address, chain: $chain}
				on_conflict: {constraint: wallets_address_chain_key, update_columns: []}
			) {
				id
				address
				chain
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"address": input.Address,
		"chain":   input.Chain,
	})

	var resp struct {
		InsertWalletsOne *Wallet `json:"insert_wallets_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	if resp.InsertWalletsOne == nil {
		// Wallet already exists, fetch it
		return c.GetWalletByAddress(ctx, input.Address, input.Chain)
	}

	return resp.InsertWalletsOne, nil
}

// DeleteWallet deletes a wallet by ID
func (c *Client) DeleteWallet(ctx context.Context, id string) error {
	query := `
		mutation DeleteWallet($id: uuid!) {
			delete_wallets_by_pk(id: $id) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		DeleteWalletsByPk struct {
			ID string `json:"id"`
		} `json:"delete_wallets_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete wallet: %w", err)
	}

	if resp.DeleteWalletsByPk.ID == "" {
		return notFoundError("wallet", id)
	}

	return nil
}

// UpdateWalletTags updates the tags for a wallet
func (c *Client) UpdateWalletTags(ctx context.Context, walletID string, tags []string) error {
	query := `
		mutation UpdateWalletTags($id: uuid!, $tags: jsonb!) {
			update_wallets_by_pk(pk_columns: {id: $id}, _set: {tags: $tags}) {
				id
				tags
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":   walletID,
		"tags": tags,
	})

	var resp struct {
		UpdateWalletsByPk *Wallet `json:"update_wallets_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update wallet tags: %w", err)
	}

	if resp.UpdateWalletsByPk == nil {
		return notFoundError("wallet", walletID)
	}

	return nil
}

// VerifyWalletByDetection sets verified_at and verification_method='detected'
// on a wallet that has not already been verified by a stronger method (signature/api_key).
// This is called by account_detector after successfully discovering exchange accounts,
// so wallets added via the dashboard don't stay unverified indefinitely.
func (c *Client) VerifyWalletByDetection(ctx context.Context, walletID string) error {
	query := `
		mutation VerifyWalletByDetection($id: uuid!, $verified_at: timestamptz!, $method: String!) {
			update_wallets(
				where: {
					id: {_eq: $id}
					verified_at: {_is_null: true}
				}
				_set: {verified_at: $verified_at, verification_method: $method}
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":          walletID,
		"verified_at": "now()",
		"method":      "detected",
	})

	var resp struct {
		UpdateWallets struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"update_wallets"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to verify wallet by detection: %w", err)
	}

	return nil
}

// UpdateWalletLastDetected updates the last_detected_at timestamp for a wallet
func (c *Client) UpdateWalletLastDetected(ctx context.Context, walletID string) error {
	query := `
		mutation UpdateWalletLastDetected($id: uuid!, $last_detected_at: timestamptz!) {
			update_wallets_by_pk(pk_columns: {id: $id}, _set: {last_detected_at: $last_detected_at}) {
				id
				last_detected_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":               walletID,
		"last_detected_at": "now()",
	})

	var resp struct {
		UpdateWalletsByPk *Wallet `json:"update_wallets_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update wallet last_detected_at: %w", err)
	}

	if resp.UpdateWalletsByPk == nil {
		return notFoundError("wallet", walletID)
	}

	return nil
}
