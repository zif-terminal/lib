package hyperliquid

import (
	"context"
	"time"
)

// maxFillsPerPage is the maximum number of fills returned per request by Hyperliquid.
const maxFillsPerPage = 2000

// maxLedgerEntriesPerPage is the number of ledger entries we expect before paginating.
const maxLedgerEntriesPerPage = 500

// clampUnixMilli returns the unix milliseconds of t, clamped to 0 (Unix epoch)
// if t is before that. Go's zero time.Time is year 1 which gives a negative ms value
// that the Hyperliquid API rejects.
func clampUnixMilli(t time.Time) int64 {
	ms := t.UnixMilli()
	if ms < 0 {
		return 0
	}
	return ms
}

// fetchAllFills fetches all fills for a user, paginating by startTime.
// since is the earliest timestamp to fetch from (unix milliseconds).
// Returns fills sorted by timestamp ascending (oldest first).
func (c *Client) fetchAllFills(ctx context.Context, user string, since time.Time) ([]hlFill, error) {
	startTime := clampUnixMilli(since)
	var all []hlFill

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req := map[string]interface{}{
			"type":      "userFillsByTime",
			"user":      user,
			"startTime": startTime,
		}

		var fills []hlFill
		if err := c.doPost(ctx, req, &fills); err != nil {
			return nil, err
		}

		if len(fills) == 0 {
			break
		}

		all = append(all, fills...)

		// If we got fewer than the max, we've reached the end
		if len(fills) < maxFillsPerPage {
			break
		}

		// Paginate: set startTime to last fill's timestamp + 1ms
		lastTime := fills[len(fills)-1].Time
		startTime = lastTime + 1
	}

	return all, nil
}

// fetchAllFunding fetches all funding entries for a user since the given time.
// Hyperliquid funding endpoint uses startTime/endTime parameters.
func (c *Client) fetchAllFunding(ctx context.Context, user string, since time.Time) ([]hlFundingEntry, error) {
	req := map[string]interface{}{
		"type":      "userFunding",
		"user":      user,
		"startTime": clampUnixMilli(since),
	}

	var entries []hlFundingEntry
	if err := c.doPost(ctx, req, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// fetchAllLedgerUpdates fetches all non-funding ledger updates for a user since the given time.
func (c *Client) fetchAllLedgerUpdates(ctx context.Context, user string, since time.Time) ([]hlLedgerEntry, error) {
	startTime := clampUnixMilli(since)
	var all []hlLedgerEntry

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req := map[string]interface{}{
			"type":      "userNonFundingLedgerUpdates",
			"user":      user,
			"startTime": startTime,
		}

		var entries []hlLedgerEntry
		if err := c.doPost(ctx, req, &entries); err != nil {
			return nil, err
		}

		if len(entries) == 0 {
			break
		}

		all = append(all, entries...)

		// If we got fewer than threshold, we've likely got everything
		if len(entries) < maxLedgerEntriesPerPage {
			break
		}

		// Paginate by setting startTime to last entry's timestamp + 1ms
		lastTime := entries[len(entries)-1].Time
		startTime = lastTime + 1
	}

	return all, nil
}
