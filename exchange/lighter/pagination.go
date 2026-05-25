package lighter

import (
	"context"
	"encoding/json"
	"fmt"
)

// defaultPageLimit is the number of records per page for Lighter API requests.
const defaultPageLimit = 100

// maxPaginationPages is a safety cap on the number of pages a single
// pagination loop will fetch before erroring out. Exists to prevent infinite
// loops if the Lighter API misbehaves (e.g. returns the same cursor
// indefinitely with non-empty pages, or never signals end-of-stream). At
// limit=100 records/page, 5000 pages = 500k records, which exceeds any
// realistic per-account history and gives plenty of headroom for legitimate
// large accounts while still bounding a runaway loop.
const maxPaginationPages = 5000

// isSuccessCode returns true if a Lighter API response code indicates success.
// The Lighter API uses code=200 for success (not 0).
func isSuccessCode(code int) bool {
	return code == 0 || code == 200
}

// fetchAllTrades fetches all pages of trades using cursor-based pagination.
//
// When sinceMs > 0, pagination stops as soon as a page's LAST (oldest) record
// has timestamp < sinceMs. Lighter's /trades feed is sorted DESCENDING by
// timestamp, so once the tail of a page is older than the cutoff, every
// subsequent page is guaranteed to be older still — there is no benefit to
// continuing. Records older than sinceMs that land on the boundary page are
// still returned and get filtered out by the caller (which is the only layer
// that knows the precise per-record `since` semantics, e.g. strictly-after vs
// at-or-after). This early-exit alone reduced full-history sync from
// 45-65min/cycle to under 5min for steady-state incremental cycles, because
// each account paginated O(history) pages even when zero new trades existed.
//
// The Lighter API ignores all of the obvious query-string filters we tried
// (start_timestamp, from_timestamp, from, since, min_timestamp, start_time,
// ts_start, begin_timestamp, timestamp_gte) — server-side filtering is not
// available, so client-side early-exit is the only viable optimisation.
func fetchAllTrades(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterTrade, error) {
	var all []lighterTrade
	cursor := ""

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter trades pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
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

		// Early-exit on since: if the oldest record on this page is already
		// older than sinceMs, every subsequent (older) page is irrelevant.
		if sinceMs > 0 && len(resp.Trades) > 0 && resp.Trades[len(resp.Trades)-1].Timestamp < sinceMs {
			break
		}

		if resp.NextCursor == "" || len(resp.Trades) == 0 {
			break
		}

		// Detect cursor non-progression: the API returned the same cursor we
		// just used. Continuing would loop forever appending duplicates. Crash
		// loudly so the bug surfaces instead of silently producing dupes.
		if resp.NextCursor == cursor {
			return nil, fmt.Errorf("lighter trades pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.NextCursor
	}

	return all, nil
}

// fetchAllDeposits fetches all pages of deposits using cursor-based pagination.
// See fetchAllTrades for the sinceMs early-exit rationale.
func fetchAllDeposits(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterDeposit, error) {
	var all []lighterDeposit
	cursor := ""

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter deposits pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
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

		if sinceMs > 0 && len(resp.Deposits) > 0 && resp.Deposits[len(resp.Deposits)-1].Timestamp < sinceMs {
			break
		}

		if resp.Cursor == "" || len(resp.Deposits) == 0 {
			break
		}

		if resp.Cursor == cursor {
			return nil, fmt.Errorf("lighter deposits pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllTransfers fetches all pages of L2 transfers using cursor-based pagination.
// See fetchAllTrades for the sinceMs early-exit rationale. NOTE: this loop is
// also used for the L1↔L2 fast-withdraw pairing logic in FetchDeposits, which
// requires transfers within ~10 minutes of L1 withdraws. The caller passes a
// sinceMs that already accounts for that pairing window.
func fetchAllTransfers(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterTransfer, error) {
	var all []lighterTransfer
	cursor := ""

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter transfers pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
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

		if sinceMs > 0 && len(resp.Transfers) > 0 && resp.Transfers[len(resp.Transfers)-1].Timestamp < sinceMs {
			break
		}

		if resp.Cursor == "" || len(resp.Transfers) == 0 {
			break
		}

		if resp.Cursor == cursor {
			return nil, fmt.Errorf("lighter transfers pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllWithdraws fetches all pages of L1 withdrawals using cursor-based pagination.
// See fetchAllTrades for the sinceMs early-exit rationale.
func fetchAllWithdraws(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterWithdraw, error) {
	var all []lighterWithdraw
	cursor := ""

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter withdraws pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
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

		if sinceMs > 0 && len(resp.Withdraws) > 0 && resp.Withdraws[len(resp.Withdraws)-1].Timestamp < sinceMs {
			break
		}

		if resp.Cursor == "" || len(resp.Withdraws) == 0 {
			break
		}

		if resp.Cursor == cursor {
			return nil, fmt.Errorf("lighter withdraws pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.Cursor
	}

	return all, nil
}

// fetchAllLiquidations fetches all pages of liquidation events using
// cursor-based pagination. Mirrors fetchAllTrades (Lighter uses next_cursor
// here, not the unprefixed cursor used by deposits/transfers/withdraws).
// See fetchAllTrades for the sinceMs early-exit rationale. Liquidations sort
// by executed_at (ms) descending, so the same boundary check applies.
func fetchAllLiquidations(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterLiquidation, error) {
	var all []lighterLiquidation
	cursor := ""

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter liquidations pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
		}

		url := urlFn(cursor)
		body, err := c.doGetWithAuth(ctx, url, authToken)
		if err != nil {
			return nil, err
		}

		if body == nil {
			break
		}

		var resp liquidationsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode liquidations response: %w", err)
		}

		if !isSuccessCode(resp.Code) {
			return nil, fmt.Errorf("API error code %d: %s", resp.Code, resp.Message)
		}

		all = append(all, resp.Liquidations...)

		if sinceMs > 0 && len(resp.Liquidations) > 0 && resp.Liquidations[len(resp.Liquidations)-1].ExecutedAt < sinceMs {
			break
		}

		if resp.NextCursor == "" || len(resp.Liquidations) == 0 {
			break
		}

		if resp.NextCursor == cursor {
			return nil, fmt.Errorf("lighter liquidations pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.NextCursor
	}

	return all, nil
}

// fetchAllFunding fetches all pages of funding payments using cursor-based
// pagination. See fetchAllTrades for the sinceMs early-exit rationale. NOTE:
// the positionFunding feed exposes timestamps in Unix SECONDS, not the
// milliseconds used by every other endpoint — we convert sinceMs to seconds
// (rounding DOWN so a sub-second boundary at the cutoff still returns the
// funding event that happens to fall on it; the caller's strictly-after
// comparison handles dedup).
func fetchAllFunding(ctx context.Context, c *Client, urlFn func(cursor string) string, authToken string, sinceMs int64) ([]lighterFunding, error) {
	var all []lighterFunding
	cursor := ""

	// Funding timestamps are seconds — convert sinceMs and round DOWN so the
	// boundary event is still included; the caller filters with strictly-after
	// time.Time comparison and will skip it correctly if it was already ingested.
	var sinceSec int64
	if sinceMs > 0 {
		sinceSec = sinceMs / 1000
	}

	for page := 0; ; page++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if page >= maxPaginationPages {
			return nil, fmt.Errorf("lighter funding pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop", maxPaginationPages, cursor, len(all))
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

		if sinceSec > 0 && len(resp.PositionFundings) > 0 && resp.PositionFundings[len(resp.PositionFundings)-1].Timestamp < sinceSec {
			break
		}

		if resp.NextCursor == "" || len(resp.PositionFundings) == 0 {
			break
		}

		if resp.NextCursor == cursor {
			return nil, fmt.Errorf("lighter funding pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop", cursor, page, len(all))
		}

		cursor = resp.NextCursor
	}

	return all, nil
}
