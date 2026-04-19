package hyperliquid

import (
	"context"
	"sort"
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

		// Paginate: set startTime to last fill's timestamp (not +1).
		// Multiple fills can share the same millisecond. Using +1 would
		// skip fills at the page boundary. We dedup by tid below.
		lastTime := fills[len(fills)-1].Time
		if lastTime == startTime {
			// All fills on this page share the same timestamp — cannot
			// paginate further without skipping. Move +1ms to avoid
			// an infinite loop. Fills at this exact ms are already captured.
			startTime = lastTime + 1
		} else {
			startTime = lastTime
		}
	}

	// Supplement with userFills (most recent 2000). userFillsByTime misses
	// certain fill types (e.g., Spot Dust Conversions) that userFills returns.
	if ctx.Err() == nil {
		var recentFills []hlFill
		recentReq := map[string]interface{}{
			"type": "userFills",
			"user": user,
		}
		if err := c.doPost(ctx, recentReq, &recentFills); err == nil {
			// Only include fills at or after the original since time
			sinceMs := clampUnixMilli(since)
			for _, f := range recentFills {
				if f.Time >= sinceMs {
					all = append(all, f)
				}
			}
		}
		// Ignore errors — userFills is supplementary
	}

	// Dedup by tid: overlapping pages and userFills may return duplicates.
	// tid=0 fills (e.g., dust conversions) use a composite key to avoid
	// collapsing distinct fills that share tid=0.
	type dedupKey struct {
		Tid  int64
		Time int64
		Coin string
	}
	seen := make(map[dedupKey]bool, len(all))
	deduped := make([]hlFill, 0, len(all))
	for _, f := range all {
		k := dedupKey{Tid: f.Tid, Time: f.Time, Coin: f.Coin}
		if f.Tid != 0 {
			k = dedupKey{Tid: f.Tid} // non-zero tid is globally unique
		}
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, f)
		}
	}

	// Sort by time ascending for consistent processing order.
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Time < deduped[j].Time
	})

	return deduped, nil
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

// maxBorrowLendEntriesPerPage is the number of borrow/lend interest entries we expect before paginating.
const maxBorrowLendEntriesPerPage = 500

// fetchAllBorrowLendInterest fetches all borrow/lend interest entries for a user since the given time.
// The API returns entries ascending by time when startTime is set.
func (c *Client) fetchAllBorrowLendInterest(ctx context.Context, user string, since time.Time) ([]hlBorrowLendInterest, error) {
	startTime := clampUnixMilli(since)
	var all []hlBorrowLendInterest

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req := map[string]interface{}{
			"type":      "userBorrowLendInterest",
			"user":      user,
			"startTime": startTime,
		}

		var entries []hlBorrowLendInterest
		if err := c.doPost(ctx, req, &entries); err != nil {
			return nil, err
		}

		if len(entries) == 0 {
			break
		}

		all = append(all, entries...)

		// If we got fewer than threshold, we've likely got everything
		if len(entries) < maxBorrowLendEntriesPerPage {
			break
		}

		// Paginate by setting startTime to last entry's timestamp + 1ms
		lastTime := entries[len(entries)-1].Time
		startTime = lastTime + 1
	}

	return all, nil
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
