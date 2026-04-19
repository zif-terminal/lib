package lighter

import (
	"context"
	"encoding/json"
	"fmt"
)

// defaultPageLimit is the number of records per page for Lighter API requests.
const defaultPageLimit = 100

// isSuccessCode returns true if a Lighter API response code indicates success.
// The Lighter API uses code=200 for success (not 0).
func isSuccessCode(code int) bool {
	return code == 0 || code == 200
}

// fetchAllTrades fetches all pages of trades using cursor-based pagination.
func fetchAllTrades(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string) ([]lighterTrade, error) {
	var all []lighterTrade
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp tradesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode trades response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Trades...)

		if resp.NextCursor == "" || len(resp.Trades) == 0 {
			break
		}

		cursor = resp.NextCursor
	}

	return all, nil
}

// fetchAllDeposits fetches all pages of deposits using cursor-based pagination.
func fetchAllDeposits(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string) ([]lighterDeposit, error) {
	var all []lighterDeposit
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp depositsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode deposits response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Deposits...)

		if resp.Cursor == "" || len(resp.Deposits) == 0 {
			break
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllTransfers fetches all pages of L2 transfers using cursor-based pagination.
func fetchAllTransfers(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string) ([]lighterTransfer, error) {
	var all []lighterTransfer
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp transfersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode transfers response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Transfers...)

		if resp.Cursor == "" || len(resp.Transfers) == 0 {
			break
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllWithdraws fetches all pages of L1 withdrawals using cursor-based pagination.
func fetchAllWithdraws(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string) ([]lighterWithdraw, error) {
	var all []lighterWithdraw
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp withdrawsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode withdraws response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Withdraws...)

		if resp.Cursor == "" || len(resp.Withdraws) == 0 {
			break
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllFunding fetches all pages of funding payments using cursor-based pagination.
func fetchAllFunding(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string) ([]lighterFunding, error) {
	var all []lighterFunding
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp fundingResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode funding response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.PositionFundings...)

		if resp.NextCursor == "" || len(resp.PositionFundings) == 0 {
			break
		}

		cursor = resp.NextCursor
	}

	return all, nil
}
