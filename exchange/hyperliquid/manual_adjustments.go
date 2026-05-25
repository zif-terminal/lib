package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/models"
)

// tagWithAdjustment stamps metadata.manual_adjustment_id (and source) onto a
// freshly transformed TransferInput when the entry it came from has a hash
// matching one of the merged manual_adjustments. No-op when:
//   - hashToAdjID is empty (the common path: no adjustments active),
//   - the hash isn't in the map (the entry came from real exchange data),
//   - the transfer is nil (transformer skipped this entry).
//
// Tagging is metadata-only — it does not change the Type / Asset / Amount /
// Timestamp the transformer produced, so adjustment-sourced and real-sourced
// rows pass through the rest of the pipeline identically. Detectors do NOT
// look at this tag; it exists purely for operator audit (and for the CLI
// `show` subcommand to verify a Transfer linked back correctly).
func tagWithAdjustment(transfer *models.TransferInput, hash string, hashToAdjID map[string]uuid.UUID) {
	if transfer == nil || len(hashToAdjID) == 0 {
		return
	}
	adjID, ok := hashToAdjID[hash]
	if !ok {
		return
	}
	if transfer.Metadata == nil {
		transfer.Metadata = make(map[string]string)
	}
	transfer.Metadata["manual_adjustment_id"] = adjID.String()
	transfer.Metadata["source"] = "manual_adjustment"
}

// adjustmentsReader is the narrow subset of db.DBClient that the HL client
// needs to fold manual adjustments into its real-data fetches. Defined as a
// local interface so tests can supply a stub without depending on the
// full DBClient surface and so the dependency direction stays one-way
// (this package depends on the db package, never the reverse).
type adjustmentsReader interface {
	FetchActiveManualAdjustments(ctx context.Context, exchangeAccountID uuid.UUID, exchange string) ([]*db.ManualAdjustment, error)
}

// fetchAdjustmentsByType pulls all active adjustments for an account, groups
// them by event_type, and returns the grouping. When the HL client has no
// dbClient configured (legacy NewClient() path used by some tests), this
// returns an empty map so callers can pass it through unchanged.
//
// The returned map keys MUST match the HL API request type strings used in
// the rest of this package:
//   - "userFunding"                 → folded into hlFundingEntry
//   - "userNonFundingLedgerUpdates" → folded into hlLedgerEntry
//   - "userFills"                   → folded into hlFill
//
// Any other key in the DB is surfaced as an error rather than silently
// dropped — adjustments are operator-authored so the operator must use a
// recognized event_type. (no-silent-unknowns rule)
func (c *Client) fetchAdjustmentsByType(ctx context.Context, accountID uuid.UUID) (map[string][]*db.ManualAdjustment, error) {
	if c.dbClient == nil {
		return map[string][]*db.ManualAdjustment{}, nil
	}
	adjustments, err := c.dbClient.FetchActiveManualAdjustments(ctx, accountID, c.Name())
	if err != nil {
		return nil, fmt.Errorf("fetch manual adjustments: %w", err)
	}
	out := make(map[string][]*db.ManualAdjustment, len(adjustments))
	for _, adj := range adjustments {
		switch adj.EventType {
		case "userFunding", "userNonFundingLedgerUpdates", "userFills",
			// Aliases used in CLI / docs — accept both forms so the CLI
			// can present friendly names while the merge code keeps using
			// the canonical HL API type strings.
			"funding", "ledger", "fills":
		default:
			return nil, fmt.Errorf("manual_adjustment %s has unrecognized event_type %q (must be one of userFunding|userNonFundingLedgerUpdates|userFills)",
				adj.ID, adj.EventType)
		}
		canonical := canonicalizeEventType(adj.EventType)
		out[canonical] = append(out[canonical], adj)
	}
	return out, nil
}

// canonicalizeEventType maps friendly aliases to the canonical HL API type
// strings used inside this package. Both forms are accepted at the CLI; the
// merge logic only deals with canonical values.
func canonicalizeEventType(t string) string {
	switch t {
	case "funding":
		return "userFunding"
	case "ledger":
		return "userNonFundingLedgerUpdates"
	case "fills":
		return "userFills"
	default:
		return t
	}
}

// ValidateAdjustmentPayload checks that payload deserializes cleanly into the
// expected struct for eventType. Called by the CLI's `add` subcommand before
// inserting a row so a malformed payload fails at write time rather than at
// the next sync cycle.
//
// Accepts both canonical HL API type strings (userFunding, etc.) and friendly
// CLI aliases (funding, etc.).
//
// Returns nil on success, a descriptive error otherwise.
func ValidateAdjustmentPayload(eventType string, payload []byte) error {
	switch canonicalizeEventType(eventType) {
	case "userFunding":
		var entry hlFundingEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			return fmt.Errorf("payload not decodable as hlFundingEntry: %w", err)
		}
		if entry.Time == 0 {
			return fmt.Errorf("hlFundingEntry.time is required (unix milliseconds)")
		}
		if entry.Hash == "" {
			return fmt.Errorf("hlFundingEntry.hash is required (use a unique synthetic value, e.g. 0x_manual_<reason>)")
		}
		if entry.Delta.Type == "" {
			return fmt.Errorf("hlFundingEntry.delta.type is required")
		}
		if entry.Delta.Coin == "" {
			return fmt.Errorf("hlFundingEntry.delta.coin is required")
		}
		if entry.Delta.Usdc == "" {
			return fmt.Errorf("hlFundingEntry.delta.usdc is required (signed decimal string)")
		}
	case "userNonFundingLedgerUpdates":
		var entry hlLedgerEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			return fmt.Errorf("payload not decodable as hlLedgerEntry: %w", err)
		}
		if entry.Time == 0 {
			return fmt.Errorf("hlLedgerEntry.time is required (unix milliseconds)")
		}
		if entry.Hash == "" {
			return fmt.Errorf("hlLedgerEntry.hash is required (use a unique synthetic value, e.g. 0x_manual_<reason>)")
		}
		if entry.Delta.Type == "" {
			return fmt.Errorf("hlLedgerEntry.delta.type is required (e.g. deposit, withdraw, spotTransfer, ...)")
		}
	case "userFills":
		var entry hlFill
		if err := json.Unmarshal(payload, &entry); err != nil {
			return fmt.Errorf("payload not decodable as hlFill: %w", err)
		}
		if entry.Time == 0 {
			return fmt.Errorf("hlFill.time is required (unix milliseconds)")
		}
		if entry.Tid == 0 {
			return fmt.Errorf("hlFill.tid is required (trade id, must be unique)")
		}
		if entry.Coin == "" {
			return fmt.Errorf("hlFill.coin is required")
		}
	default:
		return fmt.Errorf("unsupported event_type %q (must be one of: userFunding, userNonFundingLedgerUpdates, userFills — or friendly aliases funding, ledger, fills)", eventType)
	}
	return nil
}

// decodeFundingAdjustments deserializes each adjustment payload as an
// hlFundingEntry. Failures are surfaced loudly — an operator wrote the
// payload, so a malformed one means the CLI's validation was bypassed
// (or the schema drifted) and silently dropping it would re-introduce the
// gap the adjustment was supposed to close.
//
// Returns the decoded entries along with a parallel slice mapping each
// entry index back to its source adjustment UUID, so the caller can tag
// the resulting Transfer rows with metadata.manual_adjustment_id without
// having to round-trip through the entry's content.
func decodeFundingAdjustments(adjustments []*db.ManualAdjustment) ([]hlFundingEntry, []uuid.UUID, error) {
	entries := make([]hlFundingEntry, 0, len(adjustments))
	sources := make([]uuid.UUID, 0, len(adjustments))
	for _, adj := range adjustments {
		var entry hlFundingEntry
		if err := json.Unmarshal(adj.Payload, &entry); err != nil {
			return nil, nil, fmt.Errorf("manual_adjustment %s payload not decodable as hlFundingEntry: %w", adj.ID, err)
		}
		entries = append(entries, entry)
		sources = append(sources, adj.ID)
	}
	return entries, sources, nil
}

// decodeLedgerAdjustments — see decodeFundingAdjustments.
func decodeLedgerAdjustments(adjustments []*db.ManualAdjustment) ([]hlLedgerEntry, []uuid.UUID, error) {
	entries := make([]hlLedgerEntry, 0, len(adjustments))
	sources := make([]uuid.UUID, 0, len(adjustments))
	for _, adj := range adjustments {
		var entry hlLedgerEntry
		if err := json.Unmarshal(adj.Payload, &entry); err != nil {
			return nil, nil, fmt.Errorf("manual_adjustment %s payload not decodable as hlLedgerEntry: %w", adj.ID, err)
		}
		entries = append(entries, entry)
		sources = append(sources, adj.ID)
	}
	return entries, sources, nil
}

// decodeFillsAdjustments — see decodeFundingAdjustments.
func decodeFillsAdjustments(adjustments []*db.ManualAdjustment) ([]hlFill, []uuid.UUID, error) {
	entries := make([]hlFill, 0, len(adjustments))
	sources := make([]uuid.UUID, 0, len(adjustments))
	for _, adj := range adjustments {
		var entry hlFill
		if err := json.Unmarshal(adj.Payload, &entry); err != nil {
			return nil, nil, fmt.Errorf("manual_adjustment %s payload not decodable as hlFill: %w", adj.ID, err)
		}
		entries = append(entries, entry)
		sources = append(sources, adj.ID)
	}
	return entries, sources, nil
}
