package exchange

import (
	"errors"
	"testing"

	"github.com/zif-terminal/lib/exchange/iface"
)

func TestGetClient(t *testing.T) {
	t.Run("GetClient with valid exchange name", func(t *testing.T) {
		client, err := GetClient("drift")
		if err != nil {
			t.Fatalf("GetClient failed: %v", err)
		}

		if client == nil {
			t.Fatal("GetClient returned nil client")
		}

		if client.Name() != "drift" {
			t.Errorf("Expected client name 'drift', got '%s'", client.Name())
		}

		// Verify it implements the interface
		var _ iface.ExchangeClient = client
	})

	t.Run("GetClient with invalid exchange name", func(t *testing.T) {
		client, err := GetClient("nonexistent")
		if err == nil {
			t.Fatal("GetClient should return error for nonexistent exchange")
		}

		if client != nil {
			t.Error("GetClient should return nil client for nonexistent exchange")
		}

		if !errors.Is(err, ErrExchangeNotFound) {
			t.Errorf("Expected ErrExchangeNotFound, got: %v", err)
		}
	})

	t.Run("GetClient with empty name", func(t *testing.T) {
		client, err := GetClient("")
		if err == nil {
			t.Fatal("GetClient should return error for empty exchange name")
		}

		if client != nil {
			t.Error("GetClient should return nil client for empty exchange name")
		}
	})
}

func TestGetClientHyperliquid(t *testing.T) {
	client, err := GetClient("hyperliquid")
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("GetClient returned nil client")
	}

	if client.Name() != "hyperliquid" {
		t.Errorf("Expected client name 'hyperliquid', got '%s'", client.Name())
	}

	var _ iface.ExchangeClient = client
}

func TestGetClientLighter(t *testing.T) {
	client, err := GetClient("lighter")
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("GetClient returned nil client")
	}

	if client.Name() != "lighter" {
		t.Errorf("Expected client name 'lighter', got '%s'", client.Name())
	}

	var _ iface.ExchangeClient = client
}

func TestGetClientWithDB_Variational(t *testing.T) {
	// variational requires a DB client — pass nil since we're only checking construction
	client, err := GetClientWithDB("variational", nil)
	if err != nil {
		t.Fatalf("GetClientWithDB failed: %v", err)
	}

	if client == nil {
		t.Fatal("GetClientWithDB returned nil client")
	}

	if client.Name() != "variational" {
		t.Errorf("Expected client name 'variational', got '%s'", client.Name())
	}

	var _ iface.ExchangeClient = client
}

func TestGetClientWithDB_FallbackToDrift(t *testing.T) {
	client, err := GetClientWithDB("drift", nil)
	if err != nil {
		t.Fatalf("GetClientWithDB failed for drift: %v", err)
	}

	if client.Name() != "drift" {
		t.Errorf("Expected client name 'drift', got '%s'", client.Name())
	}
}

func TestGetClientWithDB_NotFound(t *testing.T) {
	_, err := GetClientWithDB("nonexistent", nil)
	if !errors.Is(err, ErrExchangeNotFound) {
		t.Errorf("Expected ErrExchangeNotFound, got: %v", err)
	}
}

func TestListAvailableExchanges(t *testing.T) {
	exchanges := ListAvailableExchanges()

	if len(exchanges) != 5 {
		t.Fatalf("Expected 5 exchanges, got %d", len(exchanges))
	}

	found := map[string]bool{}
	for _, e := range exchanges {
		found[e] = true
	}

	if !found["drift"] {
		t.Error("Expected 'drift' in exchanges list")
	}
	if !found["hyperliquid"] {
		t.Error("Expected 'hyperliquid' in exchanges list")
	}
	if !found["lighter"] {
		t.Error("Expected 'lighter' in exchanges list")
	}
	if !found["solana_dex"] {
		t.Error("Expected 'solana_dex' in exchanges list")
	}
	if !found["variational"] {
		t.Error("Expected 'variational' in exchanges list")
	}
}
