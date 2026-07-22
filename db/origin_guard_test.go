package db

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// captureServer stands up an httptest server that records every GraphQL request
// body it receives and replies with a permissive `data` payload covering all the
// mutations exercised by the #204/#240 origin-aware reset primitives. Because the
// real machinebox graphql.Client marshals {query, variables} to JSON over HTTP,
// this lets a test assert the OUTGOING query/where-clause without reaching into
// graphql.Request's unexported fields.
func captureServer(t *testing.T, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(b))
		resp := `{"data":{
			"delete_trades":{"affected_rows":1},
			"delete_transfers":{"affected_rows":1},
			"delete_settlements":{"affected_rows":1},
			"delete_spot_balance_snapshots":{"affected_rows":1},
			"delete_positions":{"affected_rows":1},
			"delete_processor_checkpoints_by_pk":{"exchange_account_id":"00000000-0000-0000-0000-000000000000"},
			"update_transfers":{"affected_rows":0},
			"insert_transfers":{"returning":[{"id":"11111111-1111-1111-1111-111111111111","exchange_account_id":"00000000-0000-0000-0000-000000000000","type":"interest","asset":"USDC","amount":"1","timestamp":0}]}
		}}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}

func newCapturingClient(url string) *Client {
	return NewClient(ClientConfig{URL: url, AdminSecret: "test-secret"})
}

// mustContainAll fails the test unless every needle is present in haystack.
func mustContainAll(t *testing.T, label, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Errorf("%s: expected query to contain %q\n--- query ---\n%s", label, n, haystack)
		}
	}
}

// TestDeleteDerivedTransfers_OriginAwarePurge asserts the force-reset purge
// primitive removes derived+manual rows but carves out the drift-hack anchor.
func TestDeleteDerivedTransfers_OriginAwarePurge(t *testing.T) {
	var bodies []string
	srv := captureServer(t, &bodies)
	defer srv.Close()
	client := newCapturingClient(srv.URL)

	if _, err := client.DeleteDerivedTransfers(context.Background(), uuid.New()); err != nil {
		t.Fatalf("DeleteDerivedTransfers failed: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(bodies))
	}
	q := bodies[0]
	// Purges the regenerable / disallowed rows...
	mustContainAll(t, "purge", q,
		`source: \"derived\"`,           // legacy interest signal (retained)
		`origin: { _in: [\"derived\", \"manual\"]`, // origin-aware purge of derived+manual
	)
	// ...while preserving the #248 drift-hack casualty anchor.
	mustContainAll(t, "drift-hack carve-out", q,
		`type: { _neq: \"hack\" }`,
		`drift-dfx-derived`,
		`drift-hack-rule`,
	)
	// It must NOT blanket-delete every derived row (that would take the anchor).
	if strings.Contains(q, `origin: { _eq: \"derived\" }`) {
		t.Errorf("purge must not blanket-match origin=derived (would delete the drift-hack anchor)")
	}
}

// TestDeleteSyncedData_PreservesUploadAndManual asserts the one blunt "delete
// everything for the account" path never wipes irreplaceable upload/manual source.
func TestDeleteSyncedData_PreservesUploadAndManual(t *testing.T) {
	var bodies []string
	srv := captureServer(t, &bodies)
	defer srv.Close()
	client := newCapturingClient(srv.URL)

	if err := client.DeleteSyncedData(context.Background(), uuid.New()); err != nil {
		t.Fatalf("DeleteSyncedData failed: %v", err)
	}

	var tradesQ, transfersQ, settlementsQ, snapshotsQ string
	for _, b := range bodies {
		switch {
		case strings.Contains(b, "delete_trades"):
			tradesQ = b
		case strings.Contains(b, "delete_transfers"):
			transfersQ = b
		case strings.Contains(b, "delete_settlements"):
			settlementsQ = b
		case strings.Contains(b, "delete_spot_balance_snapshots"):
			snapshotsQ = b
		}
	}
	for _, tc := range []struct {
		name, q string
	}{
		{"trades", tradesQ}, {"transfers", transfersQ}, {"settlements", settlementsQ},
	} {
		if tc.q == "" {
			t.Fatalf("no %s delete mutation was sent", tc.name)
		}
		mustContainAll(t, tc.name+" preserve-source", tc.q,
			`origin: { _nin: [\"upload\", \"manual\"]`,
		)
	}
	// spot_balance_snapshots has no origin column and is always re-fetchable, so
	// it is wiped whole (no _nin guard). This documents the intentional asymmetry.
	if snapshotsQ == "" {
		t.Fatalf("no snapshots delete mutation was sent")
	}
	if strings.Contains(snapshotsQ, "_nin") {
		t.Errorf("snapshots delete unexpectedly carries an origin guard: %s", snapshotsQ)
	}
}

// TestAddTransfers_StampsOrigin asserts derived writers can mark provenance and
// that raw-fact writers (empty Origin) never send the column (DB default applies).
func TestAddTransfers_StampsOrigin(t *testing.T) {
	acct := uuid.New()

	// (a) Origin set → column is sent.
	var withOrigin []string
	srvA := captureServer(t, &withOrigin)
	defer srvA.Close()
	clientA := newCapturingClient(srvA.URL)
	if _, err := clientA.AddTransfers(context.Background(), []*models.TransferInput{{
		ExchangeAccountID: acct, Type: models.TypeInterest, Asset: "USDC",
		Amount: "1", Timestamp: time.Now(), Origin: "derived",
	}}); err != nil {
		t.Fatalf("AddTransfers(origin=derived) failed: %v", err)
	}
	if len(withOrigin) == 0 || !containsOriginVar(withOrigin, "derived") {
		t.Errorf("expected insert variables to carry origin=derived, bodies=%v", withOrigin)
	}

	// (b) Origin empty → column omitted (DB default 'gateway' applies).
	var noOrigin []string
	srvB := captureServer(t, &noOrigin)
	defer srvB.Close()
	clientB := newCapturingClient(srvB.URL)
	if _, err := clientB.AddTransfers(context.Background(), []*models.TransferInput{{
		ExchangeAccountID: acct, Type: models.TypeDeposit, Asset: "USDC",
		Amount: "5", Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("AddTransfers(no origin) failed: %v", err)
	}
	for _, b := range noOrigin {
		if strings.Contains(b, "insert_transfers") && strings.Contains(b, `"origin"`) {
			t.Errorf("raw-fact insert must not send an origin column: %s", b)
		}
	}
}

// containsOriginVar reports whether any insert body carries origin=want in its
// variables payload (json-encoded).
func containsOriginVar(bodies []string, want string) bool {
	for _, b := range bodies {
		if !strings.Contains(b, "insert_transfers") {
			continue
		}
		var req struct {
			Variables struct {
				Objects []map[string]interface{} `json:"objects"`
			} `json:"variables"`
		}
		if err := json.Unmarshal([]byte(b), &req); err != nil {
			continue
		}
		for _, o := range req.Variables.Objects {
			if o["origin"] == want {
				return true
			}
		}
	}
	return false
}
