package lighter

import (
	"context"
	"encoding/json"
	"fmt"
)

// defaultPageLimit is the number of records per page for Lighter API requests.
const defaultPageLimit = 100

// fetchAllPages fetches all pages of a paginated Lighter API endpoint using cursor-based pagination.
// The urlFn is called with each cursor value (empty string for the first page) and returns the full URL.
// Returns all data items across all pages.
func fetchAllPages[T any](ctx context.Context, c *Client, urlFn func(cursor string) string) ([]T, error) {
	var all []T
	cursor := ""

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		url := urlFn(cursor)
		body, err := c.doGet(ctx, url)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp apiResponse[T]
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if resp.Code != 0 {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Data...)

		// Stop if no next cursor or no data returned
		if resp.NextCursor == "" || len(resp.Data) == 0 {
			break
		}

		cursor = resp.NextCursor
	}

	return all, nil
}
