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

// maxFundingEntriesPerPage is the number of funding entries Hyperliquid returns
// per userFunding response before we must paginate.
const maxFundingEntriesPerPage = 500

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
// Paginates by startTime, mirroring fetchAllLedgerUpdates. Funding rows for
// multiple coins share a millisecond, so we advance startTime to the last
// entry's time (not +1, which would skip same-ms boundary rows that spilled to
// the next page) and dedup by hash+coin+time across the overlapping pages.
func (c *Client) fetchAllFunding(ctx context.Context, user string, since time.Time) ([]hlFundingEntry, error) {
	startTime := clampUnixMilli(since)
	var all []hlFundingEntry

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req := map[string]interface{}{
			"type":      "userFunding",
			"user":      user,
			"startTime": startTime,
		}

		var entries []hlFundingEntry
		if err := c.doPost(ctx, req, &entries); err != nil {
			return nil, err
		}

		if len(entries) == 0 {
			break
		}

		all = append(all, entries...)

		// If we got fewer than threshold, we've got everything
		if len(entries) < maxFundingEntriesPerPage {
			break
		}

		// Advance to last entry's time (not +1) so same-ms rows that
		// spilled past the page cap aren't skipped. Dedup below removes
		// the resulting overlap.
		lastTime := entries[len(entries)-1].Time
		if lastTime == startTime {
			// Entire page shares one timestamp — cannot paginate
			// further without skipping. Advance +1ms to avoid an
			// infinite loop; rows at this ms are already captured.
			startTime = lastTime + 1
		} else {
			startTime = lastTime
		}
	}

	// Dedup by hash+coin+time: overlapping pages return duplicates.
	type dedupKey struct {
		Hash string
		Coin string
		Time int64
	}
	seen := make(map[dedupKey]bool, len(all))
	deduped := make([]hlFundingEntry, 0, len(all))
	sinceMs := clampUnixMilli(since)
	for _, e := range all {
		// Post-filter: the HL userFunding API occasionally returns entries
		// with time < startTime. Drop them so callers never see stale rows
		// that the cursor has already moved past.
		if e.Time < sinceMs {
			continue
		}
		k := dedupKey{Hash: e.Hash, Coin: e.Delta.Coin, Time: e.Time}
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, e)
		}
	}

	return deduped, nil
}

// maxBorrowLendEntriesPerPage is the number of borrow/lend interest entries we expect before paginating.
const maxBorrowLendEntriesPerPage = 500

// fetchAllBorrowLendInterest fetches all borrow/lend interest entries for a user since the given time.
// The API returns entries ascending by time when startTime is set. Multiple
// tokens settle interest at the same millisecond, so we advance startTime to
// the last entry's time (not +1, which would skip same-ms boundary rows that
// spilled to the next page) and dedup by token+time across overlapping pages.
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

		// Advance to last entry's time (not +1) so same-ms rows that
		// spilled past the page cap aren't skipped. Dedup below removes
		// the resulting overlap.
		lastTime := entries[len(entries)-1].Time
		if lastTime == startTime {
			// Entire page shares one timestamp — cannot paginate
			// further without skipping. Advance +1ms to avoid an
			// infinite loop; rows at this ms are already captured.
			startTime = lastTime + 1
		} else {
			startTime = lastTime
		}
	}

	// Dedup by token+time: overlapping pages return duplicates. This
	// matches the bli_<token>_<time> external ID built downstream.
	type dedupKey struct {
		Token string
		Time  int64
	}
	seen := make(map[dedupKey]bool, len(all))
	deduped := make([]hlBorrowLendInterest, 0, len(all))
	for _, e := range all {
		k := dedupKey{Token: e.Token, Time: e.Time}
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, e)
		}
	}

	return deduped, nil
}

// maxDelegatorHistoryEntriesPerPage is the threshold we use to decide whether
// to paginate the delegatorHistory endpoint. The endpoint may or may not page
// in practice (typical user volumes are small — tens of events at most), but
// we mirror the existing fetchAllLedgerUpdates pattern so we don't silently
// truncate heavy stakers.
const maxDelegatorHistoryEntriesPerPage = 500

// fetchAllDelegatorHistory fetches all consensus staking history entries for a
// user since the given time. We use this to surface cDeposit events (HYPE
// moving into staking) which don't appear in userNonFundingLedgerUpdates.
//
// The HL API returns entries sorted ascending by time when startTime is set.
// We advance startTime to the last entry's time (not +1, which would skip
// same-ms boundary rows that spilled to the next page) and dedup by
// hash+time+variant across overlapping pages, matching fetchAllFunding.
//
// Defensive client-side filter: in practice the delegatorHistory endpoint has
// been observed to return entries from BEFORE the requested startTime
// (re-emitting historical cDeposit/cWithdraw events on every poll). Without
// filtering, the same hash gets re-ingested every cycle and trips the
// transfers.idx_transfers_external_id_unique constraint, blocking all
// downstream sync. Mirror fetchAllFills' approach and drop entries with
// Time < sinceMs ourselves.
func (c *Client) fetchAllDelegatorHistory(ctx context.Context, user string, since time.Time) ([]hlDelegatorHistoryEntry, error) {
	sinceMs := clampUnixMilli(since)
	startTime := sinceMs
	var all []hlDelegatorHistoryEntry

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req := map[string]interface{}{
			"type":      "delegatorHistory",
			"user":      user,
			"startTime": startTime,
		}

		var entries []hlDelegatorHistoryEntry
		if err := c.doPost(ctx, req, &entries); err != nil {
			return nil, err
		}

		if len(entries) == 0 {
			break
		}

		// Drop entries the API returned despite being older than `since`.
		for _, e := range entries {
			if e.Time >= sinceMs {
				all = append(all, e)
			}
		}

		if len(entries) < maxDelegatorHistoryEntriesPerPage {
			break
		}

		// Advance to last entry's time (not +1) so same-ms rows that
		// spilled past the page cap aren't skipped. Dedup below removes
		// the resulting overlap.
		lastTime := entries[len(entries)-1].Time
		if lastTime == startTime {
			// Entire page shares one timestamp — cannot paginate
			// further without skipping. Advance +1ms to avoid an
			// infinite loop; rows at this ms are already captured.
			startTime = lastTime + 1
		} else {
			startTime = lastTime
		}
	}

	// Dedup by hash+time+variant: overlapping pages return duplicates.
	// Multiple delta variants can share a hash, so the variant
	// discriminator keeps distinct events apart (matches the
	// cdeposit_/cwithdraw_ external ID prefixes built downstream).
	type dedupKey struct {
		Hash    string
		Time    int64
		Variant string
	}
	seen := make(map[dedupKey]bool, len(all))
	deduped := make([]hlDelegatorHistoryEntry, 0, len(all))
	for _, e := range all {
		variant := ""
		switch {
		case e.Delta.CDeposit != nil:
			variant = "cDeposit"
		case e.Delta.CWithdraw != nil:
			variant = "cWithdraw"
		default:
			variant = string(e.RawDelta)
		}
		k := dedupKey{Hash: e.Hash, Time: e.Time, Variant: variant}
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, e)
		}
	}

	return deduped, nil
}

// fetchAllLedgerUpdates fetches all non-funding ledger updates for a user since
// the given time. Multiple ledger events settle at the same millisecond, so we
// advance startTime to the last entry's time (not +1, which would skip same-ms
// boundary rows that spilled to the next page) and dedup by hash+time+type
// across overlapping pages, matching fetchAllFunding.
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

		// Advance to last entry's time (not +1) so same-ms rows that
		// spilled past the page cap aren't skipped. Dedup below removes
		// the resulting overlap.
		lastTime := entries[len(entries)-1].Time
		if lastTime == startTime {
			// Entire page shares one timestamp — cannot paginate
			// further without skipping. Advance +1ms to avoid an
			// infinite loop; rows at this ms are already captured.
			startTime = lastTime + 1
		} else {
			startTime = lastTime
		}
	}

	// Dedup by hash+time+type: overlapping pages return duplicates.
	// A single hash can carry multiple delta types (e.g. vaultWithdraw +
	// vaultLeaderCommission), so type is part of the key — matching the
	// hash[/_type] external ID built downstream.
	type dedupKey struct {
		Hash string
		Time int64
		Type string
	}
	seen := make(map[dedupKey]bool, len(all))
	deduped := make([]hlLedgerEntry, 0, len(all))
	for _, e := range all {
		k := dedupKey{Hash: e.Hash, Time: e.Time, Type: e.Delta.Type}
		if !seen[k] {
			seen[k] = true
			deduped = append(deduped, e)
		}
	}

	return deduped, nil
}
