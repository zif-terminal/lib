package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/db"
	"github.com/zif-terminal/lib/models"
)

// fakeAdjustmentsReader is a test stub for the adjustmentsReader interface.
type fakeAdjustmentsReader struct {
	rows []*db.ManualAdjustment
	err  error
}

func (f *fakeAdjustmentsReader) FetchActiveManualAdjustments(ctx context.Context, accountID uuid.UUID, exchange string) ([]*db.ManualAdjustment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// TestFetchFundingPayments_MergesManualAdjustment verifies that an active
// funding-type manual_adjustment is decoded into an hlFundingEntry, merged
// with real-data fetches, run through transformFunding, and that the
// resulting TransferInput carries metadata.manual_adjustment_id linking
// back to the source row.
func TestFetchFundingPayments_MergesManualAdjustment(t *testing.T) {
	// Real-data response from the mocked HL info endpoint: one real funding
	// entry.
	realEntries := []hlFundingEntry{
		{
			Time: 1700000000000,
			Hash: "0xreal",
			Delta: hlFundingDelta{
				Coin:        "ETH",
				FundingRate: "0.0001",
				NSamples:    1,
				Usdc:        "1.00",
				Type:        "funding",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(realEntries)
	}))
	defer server.Close()

	// Manual adjustment: a separate funding entry the operator backfilled.
	adjID := uuid.New()
	adjPayload, _ := json.Marshal(hlFundingEntry{
		Time: 1702598400000, // Dec 14 2023
		Hash: "0x_manual_backfill",
		Delta: hlFundingDelta{
			Coin:        "RNDR",
			FundingRate: "0",
			NSamples:    70,
			Usdc:        "39.42",
			Type:        "funding",
		},
	})

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		dbClient: &fakeAdjustmentsReader{rows: []*db.ManualAdjustment{
			{
				ID:                adjID,
				ExchangeAccountID: uuid.New(),
				Exchange:          "hyperliquid",
				EventType:         "userFunding",
				Payload:           adjPayload,
				Reason:            "test",
			},
		}},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	payments, err := c.FetchFundingPayments(context.Background(), account, time.UnixMilli(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("expected 2 funding payments (1 real + 1 adjustment), got %d", len(payments))
	}

	// One must be tagged; one must not.
	var tagged, untagged *models.TransferInput
	for _, p := range payments {
		if _, ok := p.Metadata["manual_adjustment_id"]; ok {
			tagged = p
		} else {
			untagged = p
		}
	}
	if tagged == nil {
		t.Fatalf("expected one payment tagged with manual_adjustment_id; payments=%+v", payments)
	}
	if untagged == nil {
		t.Fatalf("expected one payment without manual_adjustment_id; payments=%+v", payments)
	}

	if tagged.Metadata["manual_adjustment_id"] != adjID.String() {
		t.Errorf("manual_adjustment_id metadata mismatch: want %s got %s", adjID, tagged.Metadata["manual_adjustment_id"])
	}
	if tagged.Metadata["source"] != "manual_adjustment" {
		t.Errorf("source metadata mismatch: want manual_adjustment got %q", tagged.Metadata["source"])
	}
	if tagged.Amount != "39.42" {
		t.Errorf("tagged amount mismatch: want 39.42 got %s", tagged.Amount)
	}
	if tagged.Asset != "USDC" {
		t.Errorf("tagged asset mismatch: want USDC got %s", tagged.Asset)
	}

	// The real entry must NOT be tagged.
	if untagged.Metadata["manual_adjustment_id"] != "" {
		t.Errorf("real entry incorrectly tagged with manual_adjustment_id=%s", untagged.Metadata["manual_adjustment_id"])
	}
}

// TestFetchFundingPayments_NoAdjustments_NoOp verifies the no-adjustments path
// is identical to legacy behaviour: same number of payments, no extra metadata.
func TestFetchFundingPayments_NoAdjustments_NoOp(t *testing.T) {
	realEntries := []hlFundingEntry{
		{
			Time: 1700000000000,
			Hash: "0xreal",
			Delta: hlFundingDelta{
				Coin: "ETH", FundingRate: "0.0001", NSamples: 1,
				Usdc: "1.00", Type: "funding",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(realEntries)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		// No dbClient — exercises the legacy path.
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0xabc",
	}
	payments, err := c.FetchFundingPayments(context.Background(), account, time.UnixMilli(0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payments) != 1 {
		t.Fatalf("expected 1 payment (real only), got %d", len(payments))
	}
	if _, ok := payments[0].Metadata["manual_adjustment_id"]; ok {
		t.Errorf("real-only payment should not carry manual_adjustment_id metadata")
	}
}

// TestFetchAdjustmentsByType_RejectsUnknownEventType verifies the no-silent-
// unknowns rule: any event_type outside the documented set must surface as an
// error rather than silently being dropped.
func TestFetchAdjustmentsByType_RejectsUnknownEventType(t *testing.T) {
	c := &Client{
		dbClient: &fakeAdjustmentsReader{rows: []*db.ManualAdjustment{
			{
				ID:        uuid.New(),
				Exchange:  "hyperliquid",
				EventType: "spotTransfer", // not a known top-level type
				Payload:   json.RawMessage(`{}`),
				Reason:    "test",
			},
		}},
	}
	_, err := c.fetchAdjustmentsByType(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown event_type")
	}
}

// TestFetchAdjustmentsByType_NoDB returns an empty map cleanly when the
// client has no dbClient (legacy NewClient path).
func TestFetchAdjustmentsByType_NoDB(t *testing.T) {
	c := &Client{}
	got, err := c.fetchAdjustmentsByType(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map when dbClient is nil, got %d entries", len(got))
	}
}

// TestCanonicalizeEventType maps friendly aliases to canonical HL API type
// strings used internally.
func TestCanonicalizeEventType(t *testing.T) {
	cases := map[string]string{
		"funding":                     "userFunding",
		"ledger":                      "userNonFundingLedgerUpdates",
		"fills":                       "userFills",
		"userFunding":                 "userFunding",
		"userNonFundingLedgerUpdates": "userNonFundingLedgerUpdates",
		"userFills":                   "userFills",
	}
	for in, want := range cases {
		got := canonicalizeEventType(in)
		if got != want {
			t.Errorf("canonicalizeEventType(%q) = %q, want %q", in, got, want)
		}
	}
}
