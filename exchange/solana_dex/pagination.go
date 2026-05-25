package solana_dex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// defaultPageLimit is the per-page transaction count Helius returns. The
// enhanced-tx endpoint accepts limit up to 100; we use 100 to minimise round
// trips. Smaller pages slow large backfills; larger requests may be silently
// capped by Helius without warning.
const defaultPageLimit = 100

// maxPaginationPages caps how many pages a single sync will fetch. At
// limit=100 records/page, 5000 pages = 500k transactions, which exceeds the
// realistic activity of any single wallet by a wide margin. The cap exists
// to crash-loudly if Helius ever stops advancing the cursor instead of
// silently looping forever and double-billing the API quota.
const maxPaginationPages = 5000

// fetchAllTransactionsMulti queries the Helius enhanced-tx endpoint for the
// wallet AND each provided ATA, then merges all pages, deduping by tx
// signature. This is required because Helius's
// `/v0/addresses/<addr>/transactions` only returns txs where <addr> appears
// in the tx's `accountData[].account` list (i.e. where <addr> is the
// authority/signer of an account that was touched). Multi-out airdrops list
// only the recipient ATA, NOT the wallet authority — so querying the wallet
// alone silently misses airdrop inflows that land on its ATAs.
//
// The dedup key is the Solana tx signature (unique on-chain). If the same
// tx is returned by both the wallet query and an ATA query, it appears
// once in the result.
//
// Order of returned txs is not guaranteed across addresses; downstream
// callers re-sort by timestamp/signature as needed.
func fetchAllTransactionsMulti(
	ctx context.Context,
	c *Client,
	addresses []string,
	untilTimestamp int64,
) ([]heliusTransaction, error) {
	seen := make(map[string]bool)
	var all []heliusTransaction
	for _, addr := range addresses {
		if addr == "" {
			continue
		}
		page, err := fetchAllTransactions(ctx, c, addr, untilTimestamp)
		if err != nil {
			return nil, fmt.Errorf("fetchAllTransactionsMulti: address %s: %w", addr, err)
		}
		for _, tx := range page {
			if seen[tx.Signature] {
				continue
			}
			seen[tx.Signature] = true
			all = append(all, tx)
		}
	}
	return all, nil
}

// fetchAllTransactions walks the Helius enhanced-tx endpoint backward in
// time using before=<oldest_signature_in_last_page>. Stops when:
//
//   1. the returned page is empty (end of stream),
//   2. all returned txs are older than `untilTimestamp` (no point continuing),
//   3. the page cap or cursor-stall guard fires (return error),
//   4. ctx is cancelled.
//
// `untilTimestamp` is in Unix seconds — pass 0 to disable the early exit.
//
// Returns transactions in API order (newest first). Caller is responsible
// for filtering / sorting / since-comparison.
func fetchAllTransactions(
	ctx context.Context,
	c *Client,
	address string,
	untilTimestamp int64,
) ([]heliusTransaction, error) {
	// Dedupe by signature across pages. Helius pagination is signature-based
	// (before=<sig>) and is supposed to be exclusive, but we've observed
	// duplicates at the page boundary in practice. Without this guard the
	// downstream `transfers` insert would hit the UNIQUE(exchange_account_id,
	// external_id) constraint and abort the whole sync.
	seenSig := make(map[string]bool)
	var all []heliusTransaction
	cursor := "" // empty cursor → first page (newest txs)

	for pageIdx := 0; ; pageIdx++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if pageIdx >= maxPaginationPages {
			return nil, fmt.Errorf("solana_dex transactions pagination: hit max page limit of %d (last cursor=%q, accumulated=%d records) — refusing to continue, possible API loop",
				maxPaginationPages, cursor, len(all))
		}

		u := fmt.Sprintf("%s/v0/addresses/%s/transactions?api-key=%s&limit=%d",
			c.baseURL, url.PathEscape(address), url.QueryEscape(c.apiKey), defaultPageLimit)
		if cursor != "" {
			u += "&before=" + url.QueryEscape(cursor)
		}

		body, err := c.doGet(ctx, u)
		if err != nil {
			return nil, err
		}
		if body == nil {
			break
		}

		var page []heliusTransaction
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("solana_dex: failed to decode tx page: %w", err)
		}

		if len(page) == 0 {
			break
		}

		for _, tx := range page {
			if seenSig[tx.Signature] {
				continue
			}
			seenSig[tx.Signature] = true
			all = append(all, tx)
		}

		// The oldest tx in the page is the last element (Helius returns newest
		// first). Use its signature as the next `before` cursor.
		oldest := page[len(page)-1]

		// Detect cursor non-progression: the new cursor equals the previous
		// one, which would loop forever. Crash loudly so the bug surfaces
		// instead of silently producing duplicates.
		if oldest.Signature == cursor && cursor != "" {
			return nil, fmt.Errorf("solana_dex transactions pagination: cursor did not advance (cursor=%q, page=%d, accumulated=%d records) — refusing to loop",
				cursor, pageIdx, len(all))
		}
		cursor = oldest.Signature

		// Early exit: if the entire page is older than `untilTimestamp`, the
		// next page will be even older — no point continuing.
		if untilTimestamp > 0 && oldest.Timestamp < untilTimestamp {
			break
		}

		// If this page was short, Helius signalled end-of-stream by giving
		// us fewer than `limit` records. Stop now.
		if len(page) < defaultPageLimit {
			break
		}
	}

	return all, nil
}
