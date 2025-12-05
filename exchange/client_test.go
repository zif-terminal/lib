package exchange

import (
	"errors"
	"testing"

	"github.com/zif-terminal/lib/exchange/iface"
)

func TestGetClient(t *testing.T) {
	t.Run("GetClient with valid exchange name", func(t *testing.T) {
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

func TestListAvailableExchanges(t *testing.T) {
	exchanges := ListAvailableExchanges()

	if len(exchanges) == 0 {
		t.Fatal("ListAvailableExchanges should return at least one exchange")
	}

	// Check that hyperliquid is in the list
	found := false
	for _, name := range exchanges {
		if name == "hyperliquid" {
			found = true
			break
		}
	}

	if !found {
		t.Error("ListAvailableExchanges should include 'hyperliquid'")
	}
}

