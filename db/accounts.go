package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// ExchangeAccount represents an exchange account model (aliased from models package)
type ExchangeAccount = models.ExchangeAccount

// ExchangeAccountInput represents exchange account input for mutations (aliased from models package)
type ExchangeAccountInput = models.ExchangeAccountInput

// GetAccount retrieves a single exchange account by ID
func (c *Client) GetAccount(ctx context.Context, id string) (*ExchangeAccount, error) {
	query := `
		query GetAccount($id: uuid!) {
			exchange_accounts_by_pk(id: $id) {
				id
				exchange_id
				account_identifier
				account_type
				account_type_metadata
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
		return nil, fmt.Errorf("account not found: %s", id)
	}

	return resp.ExchangeAccountsByPk, nil
}

// ListAccounts retrieves all exchange accounts
func (c *Client) ListAccounts(ctx context.Context) ([]*ExchangeAccount, error) {
	query := `
		query ListAccounts {
			exchange_accounts {
				id
				exchange_id
				account_identifier
				account_type
				account_type_metadata
			}
		}
	`

	req := c.graphqlRequest(query)

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
		mutation CreateAccount($exchange_id: uuid!, $account_identifier: String!, $account_type: String!, $account_type_metadata: jsonb) {
			insert_exchange_accounts_one(object: {
				exchange_id: $exchange_id
				account_identifier: $account_identifier
				account_type: $account_type
				account_type_metadata: $account_type_metadata
			}) {
				id
				exchange_id
				account_identifier
				account_type
				account_type_metadata
			}
		}
	`

	vars := map[string]interface{}{
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
				exchange_id
				account_identifier
				account_type
				account_type_metadata
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
		return nil, fmt.Errorf("account not found: %s", id)
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
		return fmt.Errorf("account not found: %s", id)
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
