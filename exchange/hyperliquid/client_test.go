package hyperliquid

import (
	"context"
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

func TestClientImplementsInterface(t *testing.T) {
	var _ iface.ExchangeClient = (*Client)(nil)
}

func TestName(t *testing.T) {
	c := NewClient()
	if c.Name() != "hyperliquid" {
		t.Errorf("expected 'hyperliquid', got %q", c.Name())
	}
}

func TestFetchSettlementsReturnsNil(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}
	settlements, err := c.FetchSettlements(ctx, account, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settlements != nil {
		t.Errorf("expected nil settlements, got %v", settlements)
	}
}

func TestFetchHistoricalBalanceSnapshotsReturnsNil(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}
	snapshots, err := c.FetchHistoricalBalanceSnapshots(ctx, account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshots != nil {
		t.Errorf("expected nil snapshots, got %v", snapshots)
	}
}

func TestTransformFill(t *testing.T) {
	accountUUID := uuid.New()

	tests := []struct {
		name       string
		fill       hlFill
		wantBase   string
		wantMarket string
		wantSide   string
	}{
		{
			name: "perp buy",
			fill: hlFill{
				Time:     1700000000000,
				Coin:     "ETH",
				Side:     "B",
				Px:       "2000.50",
				Sz:       "1.5",
				Fee:      "0.30",
				Tid:      12345,
				Oid:      100,
				FeeToken: "USDC",
			},
			wantBase:   "ETH",
			wantMarket: "perp",
			wantSide:   "buy",
		},
		{
			name: "perp sell",
			fill: hlFill{
				Time:     1700000000000,
				Coin:     "BTC",
				Side:     "A",
				Px:       "35000.00",
				Sz:       "0.1",
				Fee:      "0.50",
				Tid:      12346,
				Oid:      101,
				FeeToken: "USDC",
			},
			wantBase:   "BTC",
			wantMarket: "perp",
			wantSide:   "sell",
		},
		{
			name: "spot buy",
			fill: hlFill{
				Time:     1700000000000,
				Coin:     "SOL-SPOT",
				Side:     "B",
				Px:       "60.00",
				Sz:       "10",
				Fee:      "0.05",
				Tid:      12347,
				Oid:      102,
				FeeToken: "SOL",
			},
			wantBase:   "SOL",
			wantMarket: "spot",
			wantSide:   "buy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trade, err := transformFill(tt.fill, accountUUID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if trade.BaseAsset != tt.wantBase {
				t.Errorf("BaseAsset = %q, want %q", trade.BaseAsset, tt.wantBase)
			}
			if trade.MarketType != tt.wantMarket {
				t.Errorf("MarketType = %q, want %q", trade.MarketType, tt.wantMarket)
			}
			if trade.Side != tt.wantSide {
				t.Errorf("Side = %q, want %q", trade.Side, tt.wantSide)
			}
			if trade.QuoteAsset != "USDC" {
				t.Errorf("QuoteAsset = %q, want USDC", trade.QuoteAsset)
			}
			if trade.ExchangeAccountID != accountUUID {
				t.Errorf("ExchangeAccountID = %v, want %v", trade.ExchangeAccountID, accountUUID)
			}
		})
	}
}

func TestTransformFunding(t *testing.T) {
	accountUUID := uuid.New()

	entry := hlFundingEntry{
		Time: 1700000000000,
		Hash: "0xabc",
		Delta: hlFundingDelta{
			Coin:        "ETH",
			FundingRate: "0.0001",
			NSamples:    1,
			Usdc:        "-0.50",
		},
	}

	payment := transformFunding(entry, accountUUID)

	if payment.Type != models.TypeFunding {
		t.Errorf("Type = %q, want %q", payment.Type, models.TypeFunding)
	}
	if payment.Asset != "USDC" {
		t.Errorf("Asset = %q, want USDC", payment.Asset)
	}
	if payment.Metadata["market"] != "ETH-PERP" {
		t.Errorf("market metadata = %q, want ETH-PERP", payment.Metadata["market"])
	}
	if payment.Metadata["n_samples"] != "1" {
		t.Errorf("n_samples metadata = %q, want 1", payment.Metadata["n_samples"])
	}
	if payment.Metadata["payment_id"] != "1700000000000_ETH" {
		t.Errorf("payment_id metadata = %q, want 1700000000000_ETH", payment.Metadata["payment_id"])
	}
	if payment.Metadata["funding_rate"] != "0.0001" {
		t.Errorf("funding_rate metadata = %q, want 0.0001", payment.Metadata["funding_rate"])
	}
	if payment.Amount != "-0.5" {
		t.Errorf("Amount = %q, want -0.5 (cleanDecimal applied)", payment.Amount)
	}
}

func TestTransformFillSlashCoin(t *testing.T) {
	accountUUID := uuid.New()
	fill := hlFill{
		Time:     1700000000000,
		Coin:     "PURR/USDC",
		Side:     "B",
		Px:       "0.001",
		Sz:       "1000",
		Fee:      "0.01",
		Tid:      99,
		Oid:      50,
		FeeToken: "PURR",
	}
	trade, err := transformFill(fill, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trade.BaseAsset != "PURR" {
		t.Errorf("BaseAsset = %q, want PURR", trade.BaseAsset)
	}
	if trade.MarketType != "spot" {
		t.Errorf("MarketType = %q, want spot", trade.MarketType)
	}
	if trade.QuoteAsset != "USDC" {
		t.Errorf("QuoteAsset = %q, want USDC", trade.QuoteAsset)
	}
}

func TestTransformLedgerEntry(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	t.Run("deposit", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xabc",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "1000.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Amount != "1000" {
			t.Errorf("Amount = %q, want 1000", transfer.Amount)
		}
		if transfer.Metadata["payment_id"] != "0xabc" {
			t.Errorf("payment_id metadata = %q, want 0xabc", transfer.Metadata["payment_id"])
		}
		if transfer.Metadata["source_type"] != "deposit" {
			t.Errorf("source_type metadata = %q, want deposit", transfer.Metadata["source_type"])
		}
	})

	t.Run("withdraw", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xdef",
			Delta: hlLedgerDelta{
				Type: "withdraw",
				Usdc: "-500.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Amount != "500" {
			t.Errorf("Amount = %q, want 500", transfer.Amount)
		}
		if transfer.Metadata["payment_id"] != "0xdef" {
			t.Errorf("payment_id metadata = %q, want 0xdef", transfer.Metadata["payment_id"])
		}
	})

	t.Run("spotGenesis", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xgenesis",
			Delta: hlLedgerDelta{
				Type:   "spotGenesis",
				Token:  "PURR",
				Amount: "1000.50",
			},
		}
		transfer, price, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Asset != "PURR" {
			t.Errorf("Asset = %q, want PURR", transfer.Asset)
		}
		if transfer.Amount != "1000.5" {
			t.Errorf("Amount = %q, want 1000.5", transfer.Amount)
		}
		if transfer.Metadata["source_type"] != "spotgenesis" {
			t.Errorf("source_type = %q, want spotgenesis", transfer.Metadata["source_type"])
		}
		// spotGenesis is a free airdrop — price must be 0
		if price == nil {
			t.Fatal("expected price record for spotGenesis")
		}
		if price.Price != "0" {
			t.Errorf("price = %q, want 0 (free airdrop)", price.Price)
		}
		if price.Asset != "PURR" {
			t.Errorf("price asset = %q, want PURR", price.Asset)
		}
		if price.Denomination != "USDC" {
			t.Errorf("price denomination = %q, want USDC", price.Denomination)
		}
		if price.Source != "ledger" {
			t.Errorf("price source = %q, want ledger", price.Source)
		}
	})

	t.Run("spotTransfer inbound", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xspotin",
			Delta: hlLedgerDelta{
				Type:        "spotTransfer",
				Token:       "HYPE",
				Amount:      "50.0",
				Destination: wallet,
				User:        "0xSenderAddress",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Asset != "HYPE" {
			t.Errorf("Asset = %q, want HYPE", transfer.Asset)
		}
		if transfer.Amount != "50" {
			t.Errorf("Amount = %q, want 50", transfer.Amount)
		}
	})

	t.Run("spotTransfer outbound", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xspotout",
			Delta: hlLedgerDelta{
				Type:        "spotTransfer",
				Token:       "HYPE",
				Amount:      "25.0",
				Destination: "0xReceiverAddress",
				User:        wallet,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Asset != "HYPE" {
			t.Errorf("Asset = %q, want HYPE", transfer.Asset)
		}
	})

	t.Run("spotTransfer case insensitive match", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xspotcase",
			Delta: hlLedgerDelta{
				Type:        "spotTransfer",
				Token:       "HYPE",
				Amount:      "10.0",
				Destination: "0x4c5fed7bdda8023f3133e3a8f7c615395ad673c8", // lowercase
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want deposit (case-insensitive match)", transfer.Type)
		}
	})

	t.Run("internalTransfer outgoing", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xinternalout",
			Delta: hlLedgerDelta{
				Type:        "internalTransfer",
				Usdc:        "100.00",
				Destination: "0xOtherAddress",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "100" {
			t.Errorf("Amount = %q, want 100", transfer.Amount)
		}
	})

	t.Run("internalTransfer incoming", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xinternalin",
			Delta: hlLedgerDelta{
				Type:        "internalTransfer",
				Usdc:        "200.00",
				Destination: wallet,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Amount != "200" {
			t.Errorf("Amount = %q, want 200", transfer.Amount)
		}
	})

	t.Run("vaultCreate", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xvaultcreate",
			Delta: hlLedgerDelta{
				Type: "vaultCreate",
				Usdc: "-5000.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "5000" {
			t.Errorf("Amount = %q, want 5000", transfer.Amount)
		}
	})

	t.Run("vaultDeposit", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xvaultdeposit",
			Delta: hlLedgerDelta{
				Type: "vaultDeposit",
				Usdc: "-1000.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Amount != "1000" {
			t.Errorf("Amount = %q, want 1000", transfer.Amount)
		}
	})

	t.Run("vaultDistribution", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xvaultdist",
			Delta: hlLedgerDelta{
				Type: "vaultDistribution",
				Usdc: "250.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "250" {
			t.Errorf("Amount = %q, want 250", transfer.Amount)
		}
	})

	t.Run("accountClassTransfer skipped", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xacctclass",
			Delta: hlLedgerDelta{
				Type:   "accountClassTransfer",
				Usdc:   "100.00",
				ToPerp: true,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer != nil {
			t.Error("expected nil transfer for accountClassTransfer")
		}
	})

	t.Run("vaultWithdraw as deposit", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xvaultwithdraw",
			Delta: hlLedgerDelta{
				Type:            "vaultWithdraw",
				NetWithdrawnUsd: "5229.047513",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer for vaultWithdraw")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("expected type %q, got %q", models.TypeDeposit, transfer.Type)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("expected asset USDC, got %q", transfer.Asset)
		}
		if transfer.Amount != "5229.047513" {
			t.Errorf("expected amount 5229.047513, got %q", transfer.Amount)
		}
		if transfer.Metadata["source_type"] != "vaultwithdraw" {
			t.Errorf("expected source_type vaultwithdraw, got %q", transfer.Metadata["source_type"])
		}
		if transfer.ExternalID != "0xvaultwithdraw_vaultwithdraw" {
			t.Errorf("expected disambiguated external_id, got %q", transfer.ExternalID)
		}
	})

	t.Run("vaultLeaderCommission", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xvlc",
			Delta: hlLedgerDelta{
				Type: "vaultLeaderCommission",
				Usdc: "88.403549",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeReward {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeReward)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "88.403549" {
			t.Errorf("Amount = %q, want 88.403549", transfer.Amount)
		}
	})

	t.Run("rewardsClaim", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xrc",
			Delta: hlLedgerDelta{
				Type: "rewardsClaim",
				Usdc: "50.0",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeReward {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeReward)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "50" {
			t.Errorf("Amount = %q, want 50", transfer.Amount)
		}
	})

	t.Run("send outgoing USDC", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xsendout",
			Delta: hlLedgerDelta{
				Type:        "send",
				Usdc:        "100.0",
				Destination: "0xOtherAddress",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "100" {
			t.Errorf("Amount = %q, want 100", transfer.Amount)
		}
	})

	t.Run("send incoming USDC", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xsendin",
			Delta: hlLedgerDelta{
				Type:        "send",
				Usdc:        "200.0",
				Destination: wallet,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "200" {
			t.Errorf("Amount = %q, want 200", transfer.Amount)
		}
	})

	t.Run("send outgoing spot token", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xsendtokenout",
			Delta: hlLedgerDelta{
				Type:        "send",
				Token:       "PURR",
				Amount:      "500.0",
				Destination: "0xOtherAddress",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeWithdraw {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
		}
		if transfer.Asset != "PURR" {
			t.Errorf("Asset = %q, want PURR", transfer.Asset)
		}
		if transfer.Amount != "500" {
			t.Errorf("Amount = %q, want 500", transfer.Amount)
		}
	})

	t.Run("send incoming spot token", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xsendtokenin",
			Delta: hlLedgerDelta{
				Type:        "send",
				Token:       "PURR",
				Amount:      "300.0",
				Destination: wallet,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeDeposit {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeDeposit)
		}
		if transfer.Asset != "PURR" {
			t.Errorf("Asset = %q, want PURR", transfer.Asset)
		}
		if transfer.Amount != "300" {
			t.Errorf("Amount = %q, want 300", transfer.Amount)
		}
	})

	t.Run("liquidation skipped", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xliq",
			Delta: hlLedgerDelta{
				Type: "liquidation",
				Usdc: "100.00",
			},
		}
		transfer, price, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if transfer != nil {
			t.Error("expected nil transfer for liquidation")
		}
		if price != nil {
			t.Error("expected nil price for liquidation")
		}
	})

	t.Run("unknown type returns error", func(t *testing.T) {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xunknown",
			Delta: hlLedgerDelta{
				Type: "futureUnknownType",
				Usdc: "100.00",
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err == nil {
			t.Fatal("expected error for unknown ledger delta type, got nil")
		}
		if transfer != nil {
			t.Errorf("expected nil transfer for unknown type, got %+v", transfer)
		}
	})
}

// TestTransformLedgerEntry_WithdrawFoldsFee verifies that the HL bridge fee
// is folded into the withdraw amount so the recorded transfer matches the
// user's actual wallet-level cashflow. Real HL payload:
//   {"type":"withdraw","usdc":"47999","fee":"1.0"} → user is out 48000 total.
func TestTransformLedgerEntry_WithdrawFoldsFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xwithdrawfee",
		Delta: hlLedgerDelta{
			Type: "withdraw",
			Usdc: "47999",
			Fee:  "1.0",
		},
	}
	transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer == nil {
		t.Fatal("expected non-nil transfer")
	}
	if transfer.Type != models.TypeWithdraw {
		t.Errorf("Type = %q, want %q", transfer.Type, models.TypeWithdraw)
	}
	if transfer.Amount != "48000" {
		t.Errorf("Amount = %q, want 48000 (fee folded in)", transfer.Amount)
	}
}

// TestTransformLedgerEntry_WithdrawSignedUsdcFoldsFee covers the case where
// HL reports usdc as a signed negative value (our historical tests assume
// this shape). Fee is absolute-value added then the sign is preserved so
// the downstream abs-value normalisation still yields 48000.
func TestTransformLedgerEntry_WithdrawSignedUsdcFoldsFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xwithdrawfeesigned",
		Delta: hlLedgerDelta{
			Type: "withdraw",
			Usdc: "-47999",
			Fee:  "1.0",
		},
	}
	transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer == nil {
		t.Fatal("expected non-nil transfer")
	}
	if transfer.Amount != "48000" {
		t.Errorf("Amount = %q, want 48000", transfer.Amount)
	}
}

// TestTransformLedgerEntry_WithdrawNoFee: empty fee string leaves amount
// unchanged.
func TestTransformLedgerEntry_WithdrawNoFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xwithdrawnofee",
		Delta: hlLedgerDelta{
			Type: "withdraw",
			Usdc: "500",
			// Fee unset
		},
	}
	transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Amount != "500" {
		t.Errorf("Amount = %q, want 500 (unchanged when fee empty)", transfer.Amount)
	}
}

// TestTransformLedgerEntry_WithdrawZeroFee: fee="0" or "0.0" is a no-op.
func TestTransformLedgerEntry_WithdrawZeroFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	for _, feeStr := range []string{"0", "0.0", "0.00000"} {
		entry := hlLedgerEntry{
			Time: 1700000000000,
			Hash: "0xwithdrawzerofee_" + feeStr,
			Delta: hlLedgerDelta{
				Type: "withdraw",
				Usdc: "1234.5",
				Fee:  feeStr,
			},
		}
		transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
		if err != nil {
			t.Fatalf("fee=%q: unexpected error: %v", feeStr, err)
		}
		if transfer.Amount != "1234.5" {
			t.Errorf("fee=%q: Amount = %q, want 1234.5", feeStr, transfer.Amount)
		}
	}
}

// TestTransformLedgerEntry_DepositNoFee: baseline — deposit with no fee is
// unchanged.
func TestTransformLedgerEntry_DepositNoFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xdepnofee",
		Delta: hlLedgerDelta{
			Type: "deposit",
			Usdc: "2500.00",
		},
	}
	transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Amount != "2500" {
		t.Errorf("Amount = %q, want 2500", transfer.Amount)
	}
}

// TestTransformLedgerEntry_DepositNonZeroFee: unusual but defensively
// handled — the fee is folded into the deposit amount and a warning is
// logged. We don't assert the log text (stable-log-assertion is brittle),
// but we do assert the fold arithmetic is correct so we notice if the
// behaviour changes silently.
func TestTransformLedgerEntry_DepositNonZeroFee(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xdepfee",
		Delta: hlLedgerDelta{
			Type: "deposit",
			Usdc: "1000",
			Fee:  "0.5",
		},
	}
	transfer, _, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Amount != "1000.5" {
		t.Errorf("Amount = %q, want 1000.5 (fee folded)", transfer.Amount)
	}
}

func TestCleanDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "0"},
		{"0", "0"},
		{"1.0000", "1"},
		{"1.2300", "1.23"},
		{"-", "0"},
		{"100", "100"},
		{"-0.50", "-0.5"},
	}

	for _, tt := range tests {
		got := cleanDecimal(tt.input)
		if got != tt.want {
			t.Errorf("cleanDecimal(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFetchTradesWithMockServer(t *testing.T) {
	fills := []hlFill{
		{
			Time:     1700000000000,
			Coin:     "ETH",
			Side:     "B",
			Px:       "2000.00",
			Sz:       "1.0",
			Fee:      "0.20",
			Tid:      1,
			Oid:      10,
			FeeToken: "USDC",
		},
		{
			Time:     1700000001000,
			Coin:     "BTC",
			Side:     "A",
			Px:       "35000.00",
			Sz:       "0.5",
			Fee:      "0.50",
			Tid:      2,
			Oid:      11,
			FeeToken: "USDC",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fills)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	trades, prices, err := c.FetchTrades(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(prices) != 2 {
		t.Errorf("expected 2 price records from execution prices, got %d", len(prices))
	}
	if len(prices) > 0 {
		if prices[0].Source != "execution" {
			t.Errorf("price source = %s, want execution", prices[0].Source)
		}
		if prices[0].Asset != "ETH" {
			t.Errorf("price asset = %s, want ETH", prices[0].Asset)
		}
		if prices[0].Price != "2000" {
			t.Errorf("price = %s, want 2000", prices[0].Price)
		}
	}

	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}

	// Verify sorted ascending
	if !trades[0].Timestamp.Before(trades[1].Timestamp) {
		t.Error("trades should be sorted ascending by timestamp")
	}

	if trades[0].BaseAsset != "ETH" {
		t.Errorf("first trade base asset = %q, want ETH", trades[0].BaseAsset)
	}
	if trades[0].Side != "buy" {
		t.Errorf("first trade side = %q, want buy", trades[0].Side)
	}
}

func TestFetchFundingPaymentsWithMockServer(t *testing.T) {
	entries := []hlFundingEntry{
		{
			Time: 1700000000000,
			Hash: "0xabc",
			Delta: hlFundingDelta{
				Coin:        "ETH",
				FundingRate: "0.0001",
				NSamples:    4,
				Usdc:        "-0.50",
				Type:        "funding",
			},
		},
		{
			Time: 1700000001000,
			Hash: "0xdef",
			Delta: hlFundingDelta{
				Coin:        "BTC",
				FundingRate: "0.00005",
				NSamples:    1,
				Usdc:        "1.25",
				Type:        "funding",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	payments, err := c.FetchFundingPayments(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(payments) != 2 {
		t.Fatalf("expected 2 payments, got %d", len(payments))
	}

	// Verify sorted ascending
	if !payments[0].Timestamp.Before(payments[1].Timestamp) {
		t.Error("payments should be sorted ascending by timestamp")
	}

	// Verify first payment
	p := payments[0]
	if p.Type != models.TypeFunding {
		t.Errorf("Type = %q, want %q", p.Type, models.TypeFunding)
	}
	if p.Asset != "USDC" {
		t.Errorf("Asset = %q, want USDC", p.Asset)
	}
	if p.Amount != "-0.5" {
		t.Errorf("Amount = %q, want -0.5", p.Amount)
	}
	if p.Metadata["market"] != "ETH-PERP" {
		t.Errorf("market metadata = %q, want ETH-PERP", p.Metadata["market"])
	}
	if p.Metadata["n_samples"] != "4" {
		t.Errorf("n_samples metadata = %q, want 4", p.Metadata["n_samples"])
	}
	if p.Metadata["payment_id"] != "1700000000000_ETH" {
		t.Errorf("payment_id metadata = %q, want 1700000000000_ETH", p.Metadata["payment_id"])
	}
	if p.ExchangeAccountID != accountID {
		t.Errorf("ExchangeAccountID = %v, want %v", p.ExchangeAccountID, accountID)
	}
}

func TestFetchDepositsWithMockServer(t *testing.T) {
	entries := []hlLedgerEntry{
		{
			Time: 1700000000000,
			Hash: "0xaaa",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "1000.00",
			},
		},
		{
			Time: 1700000001000,
			Hash: "0xbbb",
			Delta: hlLedgerDelta{
				Type: "withdraw",
				Usdc: "-500.00",
			},
		},
		{
			Time: 1700000002000,
			Hash: "0xccc",
			Delta: hlLedgerDelta{
				Type:        "internalTransfer",
				Usdc:        "200.00",
				Destination: "0x1234567890abcdef1234567890abcdef12345678",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "userNonFundingLedgerUpdates":
			json.NewEncoder(w).Encode(entries)
		case "userBorrowLendInterest":
			json.NewEncoder(w).Encode([]hlBorrowLendInterest{})
		default:
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// These entries (deposit, withdraw, internalTransfer) have no spot price records
	if len(prices) != 0 {
		t.Errorf("expected 0 prices for non-spot ledger entries, got %d", len(prices))
	}

	// internalTransfer with positive USDC is now handled as a deposit
	if len(transfers) != 3 {
		t.Fatalf("expected 3 transfers (deposit + withdraw + internalTransfer), got %d", len(transfers))
	}

	// Verify sorted ascending
	if !transfers[0].Timestamp.Before(transfers[1].Timestamp) {
		t.Error("transfers should be sorted ascending by timestamp")
	}

	// Verify deposit
	dep := transfers[0]
	if dep.Type != models.TypeDeposit {
		t.Errorf("deposit Type = %q, want %q", dep.Type, models.TypeDeposit)
	}
	if dep.Asset != "USDC" {
		t.Errorf("deposit Asset = %q, want USDC", dep.Asset)
	}
	if dep.Amount != "1000" {
		t.Errorf("deposit Amount = %q, want 1000", dep.Amount)
	}
	if dep.ExchangeAccountID != accountID {
		t.Errorf("deposit ExchangeAccountID = %v, want %v", dep.ExchangeAccountID, accountID)
	}

	// Verify withdrawal
	wd := transfers[1]
	if wd.Type != models.TypeWithdraw {
		t.Errorf("withdraw Type = %q, want %q", wd.Type, models.TypeWithdraw)
	}
	if wd.Amount != "500" {
		t.Errorf("withdraw Amount = %q, want 500 (should be positive)", wd.Amount)
	}
}

func TestDiscoverAccountsWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
					TotalRawUsd  string `json:"totalRawUsd"`
				}{
					AccountValue: "5500.50",
					TotalRawUsd:  "5000.50",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{})
		case "subAccounts":
			w.Write([]byte("null"))
		case "userVaultEquities":
			w.Write([]byte("null"))
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	wallet := "0x1234567890abcdef1234567890abcdef12345678"
	accounts, err := c.DiscoverAccounts(context.Background(), wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	acc := accounts[0]
	if acc.AccountIdentifier != wallet {
		t.Errorf("AccountIdentifier = %q, want %q", acc.AccountIdentifier, wallet)
	}
	if acc.AccountType != "main" {
		t.Errorf("AccountType = %q, want main", acc.AccountType)
	}
	if acc.Name != "Main Account" {
		t.Errorf("Name = %q, want Main Account", acc.Name)
	}
}

func TestDiscoverAccountsNoActivityWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
					TotalRawUsd  string `json:"totalRawUsd"`
				}{
					AccountValue: "0.00",
					TotalRawUsd:  "0.00",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{})
		case "userNonFundingLedgerUpdates":
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accounts, err := c.DiscoverAccounts(context.Background(), "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if accounts != nil {
		t.Errorf("expected nil accounts for inactive wallet, got %d", len(accounts))
	}
}

func TestDiscoverAccountsViaLedgerHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			// Zero balance — wallet has withdrawn everything
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
					TotalRawUsd  string `json:"totalRawUsd"`
				}{
					AccountValue: "0.00",
					TotalRawUsd:  "0.00",
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{})
		case "userNonFundingLedgerUpdates":
			// Has historical activity — deposited, traded, then withdrew
			json.NewEncoder(w).Encode([]interface{}{
				map[string]interface{}{
					"time": 1700000000000,
					"hash": "0xabc",
					"delta": map[string]interface{}{
						"type": "deposit",
						"usdc": "1000.00",
					},
				},
			})
		case "subAccounts":
			w.Write([]byte("null"))
		case "userVaultEquities":
			w.Write([]byte("null"))
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	wallet := "0x83C362925a1821FCdB1d250Fef0b2dBab2098e98"
	accounts, err := c.DiscoverAccounts(context.Background(), wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account for wallet with historical activity, got %d", len(accounts))
	}

	acc := accounts[0]
	if acc.AccountIdentifier != wallet {
		t.Errorf("AccountIdentifier = %q, want %q", acc.AccountIdentifier, wallet)
	}
	if acc.AccountType != "main" {
		t.Errorf("AccountType = %q, want main", acc.AccountType)
	}
}

func TestFetchBalancesWithMockServer(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "clearinghouseState":
			json.NewEncoder(w).Encode(hlClearinghouseState{
				MarginSummary: struct {
					AccountValue string `json:"accountValue"`
					TotalRawUsd  string `json:"totalRawUsd"`
				}{
					AccountValue: "5500.50", // includes unrealized PnL — should NOT be used
					TotalRawUsd:  "5000.50", // raw USDC margin — this is what we want
				},
			})
		case "spotClearinghouseState":
			json.NewEncoder(w).Encode(hlSpotClearinghouseState{
				Balances: []struct {
					Coin  string `json:"coin"`
					Total string `json:"total"`
					Hold  string `json:"hold"`
				}{
					{Coin: "USDC", Total: "200.25", Hold: "0"},
					{Coin: "ETH", Total: "1.5", Hold: "0"},
				},
			})
		}
		requestCount++
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	account := &models.ExchangeAccount{
		ID:                uuid.New().String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	balances, err := c.FetchBalances(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(balances) != 2 {
		t.Fatalf("expected 2 balances (USDC + ETH), got %d", len(balances))
	}

	// Find USDC and ETH balances
	var usdcBalance, ethBalance *models.BalanceSnapshot
	for _, b := range balances {
		switch b.Asset {
		case "USDC":
			usdcBalance = b
		case "ETH":
			ethBalance = b
		}
	}

	if usdcBalance == nil {
		t.Fatal("expected USDC balance")
	}
	// Combined: perp totalRawUsd (5000.50) + spot USDC (200.25) = 5200.75
	if usdcBalance.Balance != "5200.75" {
		t.Errorf("USDC balance = %s, want 5200.75 (perp 5000.50 + spot 200.25)", usdcBalance.Balance)
	}

	if ethBalance == nil {
		t.Fatal("expected ETH balance")
	}
	if ethBalance.Balance != "1.5" {
		t.Errorf("ETH balance = %s, want 1.5", ethBalance.Balance)
	}
}

func TestTransformFillAtIndexedSpotCoin(t *testing.T) {
	accountUUID := uuid.New()
	fill := hlFill{
		Time:     1700000000000,
		Coin:     "@13-SPOT",
		Side:     "B",
		Px:       "25.00",
		Sz:       "10",
		Fee:      "0.01",
		Tid:      200,
		Oid:      300,
		FeeToken: "USDC",
	}
	trade, err := transformFill(fill, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before resolution, base asset should be @13
	if trade.BaseAsset != "@13" {
		t.Errorf("BaseAsset = %q, want @13 (before resolution)", trade.BaseAsset)
	}
	if trade.MarketType != "spot" {
		t.Errorf("MarketType = %q, want spot", trade.MarketType)
	}
}

func spotMetaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType, _ := body["type"].(string)
		switch reqType {
		case "spotMeta":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"tokens": []map[string]interface{}{
					{"name": "USDC", "index": 0, "szDecimals": 2, "weiDecimals": 6, "tokenId": "0x0", "isCanonical": true},
					{"name": "PURR", "index": 1, "szDecimals": 0, "weiDecimals": 18, "tokenId": "0x1", "isCanonical": true},
					{"name": "HYPE", "index": 2, "szDecimals": 2, "weiDecimals": 18, "tokenId": "0x2", "isCanonical": false},
				},
				"universe": []map[string]interface{}{
					{"name": "PURR/USDC", "tokens": []int{1, 0}, "index": 0, "isCanonical": true},
					{"name": "@1", "tokens": []int{2, 0}, "index": 1, "isCanonical": false},
				},
			})
		case "userFillsByTime":
			json.NewEncoder(w).Encode([]hlFill{
				{
					Time:     1700000000000,
					Coin:     "@1-SPOT",
					Side:     "B",
					Px:       "25.00",
					Sz:       "10",
					Fee:      "0.01",
					Tid:      200,
					Oid:      300,
					FeeToken: "HYPE",
				},
				{
					Time:     1700000001000,
					Coin:     "PURR/USDC",
					Side:     "A",
					Px:       "0.001",
					Sz:       "1000",
					Fee:      "0.005",
					Tid:      201,
					Oid:      301,
					FeeToken: "USDC",
				},
				{
					Time:     1700000002000,
					Coin:     "ETH",
					Side:     "B",
					Px:       "2000.00",
					Sz:       "1",
					Fee:      "0.20",
					Tid:      202,
					Oid:      302,
					FeeToken: "USDC",
				},
			})
		}
	}
}

func TestFetchTradesResolvesAtIndexedSpotCoins(t *testing.T) {
	resetSpotMetaCache()

	server := httptest.NewServer(spotMetaHandler())
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	trades, prices, err := c.FetchTrades(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(trades))
	}

	// First trade: @1-SPOT should resolve to HYPE/USDC
	if trades[0].BaseAsset != "HYPE" {
		t.Errorf("trade[0] BaseAsset = %q, want HYPE (@1 resolved)", trades[0].BaseAsset)
	}
	if trades[0].QuoteAsset != "USDC" {
		t.Errorf("trade[0] QuoteAsset = %q, want USDC", trades[0].QuoteAsset)
	}
	if trades[0].MarketType != "spot" {
		t.Errorf("trade[0] MarketType = %q, want spot", trades[0].MarketType)
	}

	// Second trade: PURR/USDC canonical pair
	if trades[1].BaseAsset != "PURR" {
		t.Errorf("trade[1] BaseAsset = %q, want PURR", trades[1].BaseAsset)
	}
	if trades[1].MarketType != "spot" {
		t.Errorf("trade[1] MarketType = %q, want spot", trades[1].MarketType)
	}

	// Third trade: ETH perp — should be unchanged
	if trades[2].BaseAsset != "ETH" {
		t.Errorf("trade[2] BaseAsset = %q, want ETH", trades[2].BaseAsset)
	}
	if trades[2].MarketType != "perp" {
		t.Errorf("trade[2] MarketType = %q, want perp", trades[2].MarketType)
	}

	// Price records should also use resolved names
	if len(prices) != 3 {
		t.Fatalf("expected 3 price records, got %d", len(prices))
	}
	if prices[0].Asset != "HYPE" {
		t.Errorf("prices[0] Asset = %q, want HYPE", prices[0].Asset)
	}
}

func TestSpotMetaCacheLazyLoad(t *testing.T) {
	resetSpotMetaCache()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		if body["type"] == "spotMeta" {
			callCount++
			json.NewEncoder(w).Encode(map[string]interface{}{
				"tokens": []map[string]interface{}{
					{"name": "USDC", "index": 0},
					{"name": "SOL", "index": 1},
				},
				"universe": []map[string]interface{}{
					{"name": "@0", "tokens": []int{1, 0}, "index": 0},
				},
			})
			return
		}
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()

	// First call should trigger API fetch
	base, quote, err := c.resolveSpotCoin(ctx, "@0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != "SOL" {
		t.Errorf("base = %q, want SOL", base)
	}
	if quote != "USDC" {
		t.Errorf("quote = %q, want USDC", quote)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call should use cache — no additional API call
	base2, _, err := c.resolveSpotCoin(ctx, "@0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base2 != "SOL" {
		t.Errorf("base = %q, want SOL (cached)", base2)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 API call (cached), got %d", callCount)
	}

	// Non-indexed coin should pass through without API call
	base3, quote3, err := c.resolveSpotCoin(ctx, "ETH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base3 != "ETH" {
		t.Errorf("base = %q, want ETH (passthrough)", base3)
	}
	if quote3 != "USDC" {
		t.Errorf("quote = %q, want USDC (passthrough)", quote3)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 API call, got %d", callCount)
	}
}

func TestTransformFillSpotDustConversion(t *testing.T) {
	accountUUID := uuid.New()

	// A spot dust conversion fill — should be classified as spot, not perp
	fill := hlFill{
		Time:          1728345600098,
		Coin:          "@74",
		Side:          "A",
		Px:            "0.0014717",
		Sz:            "639.282608",
		Fee:           "0",
		Tid:           0,
		ClosedPnl:     "0.0",
		StartPosition: "639.282608",
		Dir:           "Spot Dust Conversion",
		Oid:           40950466295,
	}

	trade, err := transformFill(fill, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.MarketType != "spot" {
		t.Errorf("MarketType = %q, want spot (dust conversion should be spot)", trade.MarketType)
	}
	if trade.Side != "sell" {
		t.Errorf("Side = %q, want sell", trade.Side)
	}
	// BaseAsset is still @74 before resolution
	if trade.BaseAsset != "@74" {
		t.Errorf("BaseAsset = %q, want @74 (before resolution)", trade.BaseAsset)
	}
	// Tid=0 fills must get a deterministic hash-based trade ID, not "0"
	if trade.TradeID == "0" {
		t.Error("TradeID should not be \"0\" for dust conversion fills with Tid=0")
	}
	if !strings.HasPrefix(trade.TradeID, "hl_") {
		t.Errorf("TradeID = %q, want prefix \"hl_\" for Tid=0 fills", trade.TradeID)
	}
}

func TestTransformFillTidZeroDeterministicUnique(t *testing.T) {
	accountUUID := uuid.New()

	fill1 := hlFill{
		Time:          1728345600098,
		Coin:          "@74",
		Side:          "A",
		Px:            "0.0014717",
		Sz:            "639.282608",
		Fee:           "0",
		Tid:           0,
		ClosedPnl:     "0.0",
		StartPosition: "639.282608",
		Dir:           "Spot Dust Conversion",
		Oid:           40950466295,
	}

	fill2 := hlFill{
		Time:          1728345700000,
		Coin:          "@75",
		Side:          "A",
		Px:            "0.002",
		Sz:            "100.0",
		Fee:           "0",
		Tid:           0,
		ClosedPnl:     "0.0",
		StartPosition: "100.0",
		Dir:           "Spot Dust Conversion",
		Oid:           40950466300,
	}

	trade1, err := transformFill(fill1, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trade2, err := transformFill(fill2, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade1.TradeID == "0" || trade2.TradeID == "0" {
		t.Fatal("TradeID should not be \"0\" for Tid=0 fills")
	}
	if !strings.HasPrefix(trade1.TradeID, "hl_") {
		t.Errorf("trade1 TradeID = %q, want prefix \"hl_\"", trade1.TradeID)
	}
	if !strings.HasPrefix(trade2.TradeID, "hl_") {
		t.Errorf("trade2 TradeID = %q, want prefix \"hl_\"", trade2.TradeID)
	}
	if trade1.TradeID == trade2.TradeID {
		t.Errorf("two different fills with Tid=0 produced the same TradeID: %s", trade1.TradeID)
	}
}

func TestSynthesizePrelaunchOpenings(t *testing.T) {
	accountUUID := uuid.New()

	t.Run("pre-launch perp with no buy fills", func(t *testing.T) {
		// Use a plain perp coin (e.g., HYPE) — not @N which is now classified as spot.
		// Pre-launch perps use plain symbol names from the meta response.
		fills := []hlFill{
			{
				Time:          1733265316756,
				Coin:          "HYPE",
				Side:          "A",
				Px:            "11.2",
				Sz:            "530.49",
				Fee:           "0.594148",
				Tid:           544611872761152,
				ClosedPnl:     "5941.482696",
				StartPosition: "1567.77",
				Dir:           "Sell",
			},
			{
				Time:          1733265324007,
				Coin:          "HYPE",
				Side:          "A",
				Px:            "11.2",
				Sz:            "863.1",
				Fee:           "0.966672",
				Tid:           884529762032827,
				ClosedPnl:     "9666.711369",
				StartPosition: "1037.28",
				Dir:           "Sell",
			},
		}

		result := synthesizePrelaunchOpenings(fills, accountUUID)

		// Should have 3 fills: 1 synthetic + 2 original
		if len(result) != 3 {
			t.Fatalf("expected 3 fills, got %d", len(result))
		}

		// Find the synthetic fill
		var synth *hlFill
		for i := range result {
			if result[i].Dir == "Synthetic Open Long" {
				synth = &result[i]
				break
			}
		}
		if synth == nil {
			t.Fatal("expected a synthetic opening fill")
		}

		if synth.Sz != "1567.77" {
			t.Errorf("synthetic Sz = %q, want 1567.77 (startPosition of earliest fill)", synth.Sz)
		}
		if synth.Px != "0" {
			t.Errorf("synthetic Px = %q, want 0", synth.Px)
		}
		if synth.Side != "B" {
			t.Errorf("synthetic Side = %q, want B (buy)", synth.Side)
		}
		if synth.Time >= 1733265316756 {
			t.Errorf("synthetic Time = %d, should be before earliest fill", synth.Time)
		}
	})

	t.Run("spot dust conversion skipped — opening comes from spotGenesis transfer", func(t *testing.T) {
		fills := []hlFill{
			{
				Time:          1728345600098,
				Coin:          "@74",
				Side:          "A",
				Px:            "0.0014717",
				Sz:            "639.282608",
				Fee:           "0",
				Tid:           0,
				ClosedPnl:     "0.0",
				StartPosition: "639.282608",
				Dir:           "Spot Dust Conversion",
			},
		}

		result := synthesizePrelaunchOpenings(fills, accountUUID)

		// Dust conversions should NOT get synthetic fills — the opening
		// position is captured via spotGenesis ledger entries (deposit transfers).
		if len(result) != 1 {
			t.Fatalf("expected 1 fill (no synthesis for dust conversion), got %d", len(result))
		}
	})

	t.Run("coin with buy fills is not synthesized", func(t *testing.T) {
		fills := []hlFill{
			{
				Time: 1700000000000,
				Coin: "ETH",
				Side: "B",
				Px:   "2000",
				Sz:   "1",
				Dir:  "Open Long",
			},
			{
				Time: 1700000001000,
				Coin: "ETH",
				Side: "A",
				Px:   "2100",
				Sz:   "1",
				Dir:  "Close Long",
				StartPosition: "1",
			},
		}

		result := synthesizePrelaunchOpenings(fills, accountUUID)
		if len(result) != 2 {
			t.Errorf("expected 2 fills (no synthesis for coin with buys), got %d", len(result))
		}
	})

	t.Run("no synthesis for zero startPosition", func(t *testing.T) {
		fills := []hlFill{
			{
				Time:          1700000000000,
				Coin:          "@50",
				Side:          "A",
				Px:            "1.0",
				Sz:            "10",
				Dir:           "Spot Dust Conversion",
				StartPosition: "0",
			},
		}

		result := synthesizePrelaunchOpenings(fills, accountUUID)
		if len(result) != 1 {
			t.Errorf("expected 1 fill (no synthesis for zero startPosition), got %d", len(result))
		}
	})
}

func TestDoPostRateLimitRetry(t *testing.T) {
	var retryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := retryCount.Add(1)
		if attempt <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Succeed on 3rd attempt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hlFill{})
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result []hlFill
	err := c.doPost(ctx, map[string]string{"type": "userFills", "user": "0x0"}, &result)
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}

	count := retryCount.Load()
	if count != 3 {
		t.Errorf("expected 3 attempts (2 retries + 1 success), got %d", count)
	}
}

func TestDoPostRateLimitExhausted(t *testing.T) {
	var retryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Use a generous timeout — we want to test that maxRetries is hit, not context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var result []hlFill
	err := c.doPost(ctx, map[string]string{"type": "userFills", "user": "0x0"}, &result)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if !iface.IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}

	// Should have made maxRetries+1 attempts
	count := retryCount.Load()
	if count != int32(maxRetries+1) {
		t.Errorf("expected %d attempts, got %d", maxRetries+1, count)
	}
}

func TestDoPostRespectsRetryAfterHeader(t *testing.T) {
	var timestamps []time.Time
	var retryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		attempt := retryCount.Add(1)
		if attempt == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Succeed on 2nd attempt
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hlFill{})
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result []hlFill
	err := c.doPost(ctx, map[string]string{"type": "userFills", "user": "0x0"}, &result)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(timestamps) < 2 {
		t.Fatal("expected at least 2 requests")
	}

	// The gap between requests should be at least ~1 second (the Retry-After value)
	gap := timestamps[1].Sub(timestamps[0])
	if gap < 900*time.Millisecond {
		t.Errorf("expected at least ~1s gap due to Retry-After header, got %v", gap)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", 0},
		{"1", 1 * time.Second},
		{"5", 5 * time.Second},
		{"60", 60 * time.Second},
		{"abc", 0},
		{"1.5", 0}, // Only integer seconds supported
	}

	for _, tt := range tests {
		got := parseRetryAfter(tt.input)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestTransformFillBareAtCoinIsSpot(t *testing.T) {
	accountUUID := uuid.New()

	tests := []struct {
		name     string
		coin     string
		wantBase string
		wantType string
	}{
		{
			name:     "bare @107 is spot",
			coin:     "@107",
			wantBase: "@107",
			wantType: "spot",
		},
		{
			name:     "bare @2 is spot",
			coin:     "@2",
			wantBase: "@2",
			wantType: "spot",
		},
		{
			name:     "ETH is perp",
			coin:     "ETH",
			wantBase: "ETH",
			wantType: "perp",
		},
		{
			name:     "BTC is perp",
			coin:     "BTC",
			wantBase: "BTC",
			wantType: "perp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fill := hlFill{
				Time: 1700000000000,
				Coin: tt.coin,
				Side: "B",
				Px:   "10",
				Sz:   "1",
				Fee:  "0",
				Tid:  1,
			}
			trade, err := transformFill(fill, accountUUID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if trade.BaseAsset != tt.wantBase {
				t.Errorf("BaseAsset = %q, want %q", trade.BaseAsset, tt.wantBase)
			}
			if trade.MarketType != tt.wantType {
				t.Errorf("MarketType = %q, want %q", trade.MarketType, tt.wantType)
			}
		})
	}
}

func TestSynthesizePrelaunchOpeningsSkipsBareAtCoins(t *testing.T) {
	accountUUID := uuid.New()

	// Bare @N coin with sell-only fills and startPosition > 0 — should NOT be synthesized
	// because @N coins are spot, and their opening comes from spotGenesis deposits.
	fills := []hlFill{
		{
			Time:          1733265316756,
			Coin:          "@107",
			Side:          "A",
			Px:            "25.5",
			Sz:            "100",
			Fee:           "0.1",
			Tid:           123,
			ClosedPnl:     "500",
			StartPosition: "200",
			Dir:           "Sell",
		},
	}

	result := synthesizePrelaunchOpenings(fills, accountUUID)

	if len(result) != 1 {
		t.Fatalf("expected 1 fill (no synthesis for bare @N spot coin), got %d", len(result))
	}
}

func TestTransformLedgerEntrySpotTransferWithUsdcValue(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xspotval",
		Delta: hlLedgerDelta{
			Type:        "spotTransfer",
			Token:       "HYPE",
			Amount:      "100.0",
			Destination: wallet,
			UsdcValue:   "2500.0",
		},
	}

	transfer, price, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer == nil {
		t.Fatal("expected non-nil transfer")
	}
	if transfer.Asset != "HYPE" {
		t.Errorf("Asset = %q, want HYPE", transfer.Asset)
	}
	if transfer.Amount != "100" {
		t.Errorf("Amount = %q, want 100", transfer.Amount)
	}

	// Price should be derived: 2500 / 100 = 25
	if price == nil {
		t.Fatal("expected price record for spotTransfer with usdcValue")
	}
	if price.Price != "25" {
		t.Errorf("price = %q, want 25 (2500/100)", price.Price)
	}
	if price.Asset != "HYPE" {
		t.Errorf("price asset = %q, want HYPE", price.Asset)
	}
	if price.Denomination != "USDC" {
		t.Errorf("price denomination = %q, want USDC", price.Denomination)
	}
	if price.Source != "ledger" {
		t.Errorf("price source = %q, want ledger", price.Source)
	}
}

func TestTransformLedgerEntrySpotTransferWithoutUsdcValue(t *testing.T) {
	accountUUID := uuid.New()
	wallet := "0x4C5feD7BDDA8023f3133e3A8F7C615395AD673c8"

	entry := hlLedgerEntry{
		Time: 1700000000000,
		Hash: "0xspotnoval",
		Delta: hlLedgerDelta{
			Type:        "spotTransfer",
			Token:       "HYPE",
			Amount:      "50.0",
			Destination: wallet,
		},
	}

	transfer, price, err := transformLedgerEntry(entry, accountUUID, wallet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer == nil {
		t.Fatal("expected non-nil transfer")
	}
	if price != nil {
		t.Errorf("expected nil price for spotTransfer without usdcValue, got %+v", price)
	}
}

func TestTransformBorrowLendInterest(t *testing.T) {
	accountUUID := uuid.New()

	t.Run("net positive (supply > borrow)", func(t *testing.T) {
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "USDC",
			Borrow: "0.0",
			Supply: "0.03754258",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeInterest {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeInterest)
		}
		if transfer.Asset != "USDC" {
			t.Errorf("Asset = %q, want USDC", transfer.Asset)
		}
		if transfer.Amount != "0.03754258" {
			t.Errorf("Amount = %q, want 0.03754258", transfer.Amount)
		}
		if transfer.ExternalID != "bli_USDC_1700000000000" {
			t.Errorf("ExternalID = %q, want bli_USDC_1700000000000", transfer.ExternalID)
		}
		if transfer.Metadata["source_type"] != "borrow_lend_interest" {
			t.Errorf("source_type = %q, want borrow_lend_interest", transfer.Metadata["source_type"])
		}
		if transfer.Metadata["borrow"] != "0.0" {
			t.Errorf("borrow metadata = %q, want 0.0", transfer.Metadata["borrow"])
		}
		if transfer.Metadata["supply"] != "0.03754258" {
			t.Errorf("supply metadata = %q, want 0.03754258", transfer.Metadata["supply"])
		}
		if transfer.ExchangeAccountID != accountUUID {
			t.Errorf("ExchangeAccountID = %v, want %v", transfer.ExchangeAccountID, accountUUID)
		}
	})

	t.Run("net negative (borrow > supply)", func(t *testing.T) {
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "HYPE",
			Borrow: "0.5",
			Supply: "0.1",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Type != models.TypeInterest {
			t.Errorf("Type = %q, want %q", transfer.Type, models.TypeInterest)
		}
		if transfer.Asset != "HYPE" {
			t.Errorf("Asset = %q, want HYPE", transfer.Asset)
		}
		// Amount should be positive (0.4), direction is from Type
		if transfer.Amount != "0.4" {
			t.Errorf("Amount = %q, want 0.4", transfer.Amount)
		}
	})

	t.Run("net zero skipped", func(t *testing.T) {
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "USDC",
			Borrow: "0.5",
			Supply: "0.5",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer != nil {
			t.Errorf("expected nil transfer for zero net interest, got %+v", transfer)
		}
	})

	t.Run("both zero skipped", func(t *testing.T) {
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "USDC",
			Borrow: "0.0",
			Supply: "0.0",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer != nil {
			t.Errorf("expected nil transfer for zero borrow and supply, got %+v", transfer)
		}
	})

	t.Run("non-USDC token", func(t *testing.T) {
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "HYPE",
			Borrow: "0.0",
			Supply: "1.23456789",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		if transfer.Asset != "HYPE" {
			t.Errorf("Asset = %q, want HYPE", transfer.Asset)
		}
		if transfer.Amount != "1.23456789" {
			t.Errorf("Amount = %q, want 1.23456789", transfer.Amount)
		}
		if transfer.ExternalID != "bli_HYPE_1700000000000" {
			t.Errorf("ExternalID = %q, want bli_HYPE_1700000000000", transfer.ExternalID)
		}
	})

	t.Run("exact arithmetic no floating point error", func(t *testing.T) {
		// These values would cause floating point errors if using float64
		entry := hlBorrowLendInterest{
			Time:   1700000000000,
			Token:  "USDC",
			Borrow: "0.1",
			Supply: "0.3",
		}
		transfer := transformBorrowLendInterest(entry, accountUUID)
		if transfer == nil {
			t.Fatal("expected non-nil transfer")
		}
		// 0.3 - 0.1 = 0.2 exactly with big.Rat
		if transfer.Amount != "0.2" {
			t.Errorf("Amount = %q, want 0.2 (exact arithmetic)", transfer.Amount)
		}
	})
}

func TestFetchDepositsIncludesBorrowLendInterest(t *testing.T) {
	ledgerEntries := []hlLedgerEntry{
		{
			Time: 1700000000000,
			Hash: "0xaaa",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "1000.00",
			},
		},
	}

	bliEntries := []hlBorrowLendInterest{
		{
			Time:   1700000001000,
			Token:  "USDC",
			Borrow: "0.0",
			Supply: "0.05",
		},
		{
			Time:   1700000002000,
			Token:  "HYPE",
			Borrow: "0.0",
			Supply: "1.5",
		},
		{
			Time:   1700000003000,
			Token:  "USDC",
			Borrow: "0.0",
			Supply: "0.0", // zero — should be skipped
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "userNonFundingLedgerUpdates":
			json.NewEncoder(w).Encode(ledgerEntries)
		case "userBorrowLendInterest":
			json.NewEncoder(w).Encode(bliEntries)
		default:
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	transfers, _, err := c.FetchDeposits(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1 deposit + 2 interest (the zero one is skipped) = 3
	if len(transfers) != 3 {
		t.Fatalf("expected 3 transfers (1 deposit + 2 interest), got %d", len(transfers))
	}

	// Verify sorted ascending
	for i := 1; i < len(transfers); i++ {
		if transfers[i].Timestamp.Before(transfers[i-1].Timestamp) {
			t.Errorf("transfers not sorted: [%d] %v before [%d] %v", i, transfers[i].Timestamp, i-1, transfers[i-1].Timestamp)
		}
	}

	// Find interest transfers
	var interestTransfers []*models.TransferInput
	for _, tr := range transfers {
		if tr.Type == models.TypeInterest {
			interestTransfers = append(interestTransfers, tr)
		}
	}
	if len(interestTransfers) != 2 {
		t.Fatalf("expected 2 interest transfers, got %d", len(interestTransfers))
	}

	// First interest: USDC
	if interestTransfers[0].Asset != "USDC" {
		t.Errorf("interest[0] Asset = %q, want USDC", interestTransfers[0].Asset)
	}
	if interestTransfers[0].Amount != "0.05" {
		t.Errorf("interest[0] Amount = %q, want 0.05", interestTransfers[0].Amount)
	}

	// Second interest: HYPE
	if interestTransfers[1].Asset != "HYPE" {
		t.Errorf("interest[1] Asset = %q, want HYPE", interestTransfers[1].Asset)
	}
	if interestTransfers[1].Amount != "1.5" {
		t.Errorf("interest[1] Amount = %q, want 1.5", interestTransfers[1].Amount)
	}
}

func TestFetchDepositsReturnsPriceRecords(t *testing.T) {
	entries := []hlLedgerEntry{
		{
			Time: 1700000000000,
			Hash: "0xgenesis1",
			Delta: hlLedgerDelta{
				Type:   "spotGenesis",
				Token:  "PURR",
				Amount: "1000.0",
			},
		},
		{
			Time: 1700000001000,
			Hash: "0xspottx1",
			Delta: hlLedgerDelta{
				Type:        "spotTransfer",
				Token:       "HYPE",
				Amount:      "50.0",
				Destination: "0x1234567890abcdef1234567890abcdef12345678",
				UsdcValue:   "1250.0",
			},
		},
		{
			Time: 1700000002000,
			Hash: "0xdeposit1",
			Delta: hlLedgerDelta{
				Type: "deposit",
				Usdc: "5000.0",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")

		reqType := body["type"].(string)
		switch reqType {
		case "userNonFundingLedgerUpdates":
			json.NewEncoder(w).Encode(entries)
		case "userBorrowLendInterest":
			json.NewEncoder(w).Encode([]hlBorrowLendInterest{})
		default:
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	transfers, prices, err := c.FetchDeposits(context.Background(), account, time.UnixMilli(1700000000000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(transfers) != 3 {
		t.Fatalf("expected 3 transfers, got %d", len(transfers))
	}

	// Should have 2 price records: spotGenesis (price=0) and spotTransfer (price=25)
	if len(prices) != 2 {
		t.Fatalf("expected 2 price records, got %d", len(prices))
	}

	// First price: spotGenesis PURR at price 0
	if prices[0].Asset != "PURR" {
		t.Errorf("prices[0].Asset = %q, want PURR", prices[0].Asset)
	}
	if prices[0].Price != "0" {
		t.Errorf("prices[0].Price = %q, want 0", prices[0].Price)
	}

	// Second price: spotTransfer HYPE at price 25 (1250/50)
	if prices[1].Asset != "HYPE" {
		t.Errorf("prices[1].Asset = %q, want HYPE", prices[1].Asset)
	}
	if prices[1].Price != "25" {
		t.Errorf("prices[1].Price = %q, want 25", prices[1].Price)
	}
}

// newHLFetchAccountNameServer stands up a mock Hyperliquid info server that
// responds to subAccounts requests with a fixed payload. Returns the server
// and a pointer to the number of subAccounts calls observed.
func newHLFetchAccountNameServer(t *testing.T, expectedMaster string, payload string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")

		if body["type"] != "subAccounts" {
			t.Errorf("unexpected request type: %v", body["type"])
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		atomic.AddInt32(&calls, 1)
		if expectedMaster != "" && body["user"] != expectedMaster {
			t.Errorf("unexpected user: got %v, want %q", body["user"], expectedMaster)
		}
		w.Write([]byte(payload))
	}))
	return server, &calls
}

func TestFetchAccountName_MasterReturnsEmptyWithoutHTTPCall(t *testing.T) {
	calls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Write([]byte("null"))
	}))
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "0xabc",
		AccountType:       "main",
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("expected 0 HTTP calls for master account, got %d", calls)
	}
}

func TestFetchAccountName_SubAccountMatchByAddress(t *testing.T) {
	master := "0x1111111111111111111111111111111111111111"
	sub := "0x2222222222222222222222222222222222222222"
	payload := `[
		{"subAccountUser":"0x9999999999999999999999999999999999999999","name":"Other","master":"` + master + `"},
		{"subAccountUser":"` + sub + `","name":"Trading Sub","master":"` + master + `"}
	]`
	server, calls := newHLFetchAccountNameServer(t, master, payload)
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	meta, _ := json.Marshal(map[string]interface{}{"master_wallet": master})
	acct := &models.ExchangeAccount{
		ID:                  uuid.NewString(),
		AccountIdentifier:   sub,
		AccountType:         "sub_account",
		AccountTypeMetadata: meta,
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Trading Sub" {
		t.Errorf("name = %q, want %q", name, "Trading Sub")
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("expected 1 HTTP call, got %d", *calls)
	}
}

func TestFetchAccountName_SubAccountMatchCaseInsensitive(t *testing.T) {
	master := "0x1111111111111111111111111111111111111111"
	// Account identifier lowercased; API returns uppercase — must still match.
	subUpper := "0xAAAABBBBCCCCDDDDEEEEFFFF0000111122223333"
	subLower := strings.ToLower(subUpper)
	payload := `[
		{"subAccountUser":"` + subUpper + `","name":"Mixed Case Sub","master":"` + master + `"}
	]`
	server, _ := newHLFetchAccountNameServer(t, master, payload)
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	meta, _ := json.Marshal(map[string]interface{}{"master_wallet": master})
	acct := &models.ExchangeAccount{
		ID:                  uuid.NewString(),
		AccountIdentifier:   subLower,
		AccountType:         "sub_account",
		AccountTypeMetadata: meta,
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Mixed Case Sub" {
		t.Errorf("name = %q, want %q", name, "Mixed Case Sub")
	}
}

func TestFetchAccountName_SubAccountNoMatchReturnsEmpty(t *testing.T) {
	// API returns a different subaccount; ours isn't in the list (removed upstream).
	master := "0x1111111111111111111111111111111111111111"
	payload := `[
		{"subAccountUser":"0x9999999999999999999999999999999999999999","name":"Other","master":"` + master + `"}
	]`
	server, _ := newHLFetchAccountNameServer(t, master, payload)
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	meta, _ := json.Marshal(map[string]interface{}{"master_wallet": master})
	acct := &models.ExchangeAccount{
		ID:                  uuid.NewString(),
		AccountIdentifier:   "0x2222222222222222222222222222222222222222",
		AccountType:         "sub_account",
		AccountTypeMetadata: meta,
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty (no match)", name)
	}
}

func TestFetchAccountName_SubAccountNullResponseReturnsEmpty(t *testing.T) {
	// subAccounts returns "null" (master has no subaccounts).
	master := "0x1111111111111111111111111111111111111111"
	server, _ := newHLFetchAccountNameServer(t, master, `null`)
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	meta, _ := json.Marshal(map[string]interface{}{"master_wallet": master})
	acct := &models.ExchangeAccount{
		ID:                  uuid.NewString(),
		AccountIdentifier:   "0x2222222222222222222222222222222222222222",
		AccountType:         "sub_account",
		AccountTypeMetadata: meta,
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

func TestFetchAccountName_SubAccountMissingMasterErrors(t *testing.T) {
	c := NewClient()
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "0x2222222222222222222222222222222222222222",
		AccountType:       "sub_account",
		// No AccountTypeMetadata → master_wallet missing.
	}
	_, err := c.FetchAccountName(context.Background(), acct)
	if err == nil {
		t.Fatal("expected error for missing master_wallet")
	}
}

func TestFetchAccountName_VaultReturnsEmpty(t *testing.T) {
	calls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Write([]byte("null"))
	}))
	defer server.Close()

	c := &Client{apiURL: server.URL, httpClient: &http.Client{Timeout: 5 * time.Second}}
	acct := &models.ExchangeAccount{
		ID:                uuid.NewString(),
		AccountIdentifier: "0xvault",
		AccountType:       "vault",
	}

	name, err := c.FetchAccountName(context.Background(), acct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty for vault", name)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("expected 0 HTTP calls for vault, got %d", calls)
	}
}

// TestDeriveFeeAsset exercises the strict fail-fast fee-asset derivation. The
// motivating bug: Hyperliquid spot buys charge fees in the asset RECEIVED
// (e.g., USDH on a USDH buy, USDC on a sell), but the old code hardcoded
// USDC, producing phantom +USDH / -USDC positions in the processor. We now
// reject any non-zero-fee fill with an empty feeToken rather than guess.
func TestDeriveFeeAsset(t *testing.T) {
	tests := []struct {
		name     string
		fill     hlFill
		want     string
		wantErr  bool
		errMatch string
	}{
		{
			name: "zero fee empty token defaults to USDC",
			fill: hlFill{Tid: 1, Fee: "0", FeeToken: ""},
			want: "USDC",
		},
		{
			name: "zero fee explicit token is honoured",
			fill: hlFill{Tid: 2, Fee: "0", FeeToken: "USDH"},
			want: "USDH",
		},
		{
			name: "zero fee with empty string and empty token",
			fill: hlFill{Tid: 3, Fee: "", FeeToken: ""},
			want: "USDC",
		},
		{
			name:     "non-zero fee with empty token fails loudly",
			fill:     hlFill{Tid: 42, Fee: "1.0", FeeToken: ""},
			wantErr:  true,
			errMatch: "feeToken",
		},
		{
			name: "lowercase feeToken is normalized",
			fill: hlFill{Tid: 4, Fee: "1.0", FeeToken: "usdh"},
			want: "USDH",
		},
		{
			name: "feeToken with whitespace is trimmed",
			fill: hlFill{Tid: 5, Fee: "1.0", FeeToken: " USDC "},
			want: "USDC",
		},
		{
			name: "negative fee (rebate) with token",
			fill: hlFill{Tid: 6, Fee: "-0.5", FeeToken: "USDC"},
			want: "USDC",
		},
		{
			name:     "negative fee with empty token fails loudly",
			fill:     hlFill{Tid: 7, Fee: "-0.5", FeeToken: ""},
			wantErr:  true,
			errMatch: "feeToken",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveFeeAsset(tt.fill)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (returned %q)", got)
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("deriveFeeAsset = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTransformFillRejectsMissingFeeToken verifies that a fill with a non-zero
// fee but empty feeToken causes transformFill (and therefore FetchTrades) to
// fail loudly rather than silently defaulting. This is the invariant that
// prevents the original phantom-USDH-position bug from recurring.
func TestTransformFillRejectsMissingFeeToken(t *testing.T) {
	accountUUID := uuid.New()
	fill := hlFill{
		Time:     1700000000000,
		Coin:     "ETH",
		Side:     "B",
		Px:       "2000.00",
		Sz:       "1.0",
		Fee:      "1.0",
		Tid:      42,
		Oid:      99,
		FeeToken: "", // malformed fill — should error
	}

	trade, err := transformFill(fill, accountUUID)
	if err == nil {
		t.Fatalf("expected error for missing feeToken, got trade=%+v", trade)
	}
	if trade != nil {
		t.Errorf("expected nil trade on error, got %+v", trade)
	}
	if !strings.Contains(err.Error(), "feeToken") {
		t.Errorf("error = %q, want substring 'feeToken'", err.Error())
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("error = %q, want the fill's Tid (42) to be mentioned", err.Error())
	}
}

// TestTransformFillSpotBuyHonoursFeeToken — the specific scenario from the
// production bug: a HL spot buy of USDH where the fee is charged in USDH, not
// USDC. The transform must surface FeeAsset="USDH" so the downstream processor
// debits USDH (the asset received) rather than USDC.
func TestTransformFillSpotBuyHonoursFeeToken(t *testing.T) {
	accountUUID := uuid.New()
	fill := hlFill{
		Time:     1700000000000,
		Coin:     "USDH/USDC",
		Side:     "B",
		Px:       "1.0",
		Sz:       "100",
		Fee:      "0.04",
		Tid:      777,
		Oid:      555,
		FeeToken: "USDH",
	}

	trade, err := transformFill(fill, accountUUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trade.FeeAsset != "USDH" {
		t.Errorf("FeeAsset = %q, want USDH (fee charged in asset received on spot buy)", trade.FeeAsset)
	}
	if trade.BaseAsset != "USDH" {
		t.Errorf("BaseAsset = %q, want USDH", trade.BaseAsset)
	}
	if trade.QuoteAsset != "USDC" {
		t.Errorf("QuoteAsset = %q, want USDC", trade.QuoteAsset)
	}
	if trade.MarketType != "spot" {
		t.Errorf("MarketType = %q, want spot", trade.MarketType)
	}
}

// TestFetchTradesPropagatesMalformedFillError verifies that a malformed fill
// (non-zero fee, empty feeToken) returned by the HL API causes FetchTrades to
// return an error rather than silently fabricating a USDC label.
func TestFetchTradesPropagatesMalformedFillError(t *testing.T) {
	malformed := []hlFill{
		{
			Time:     1700000000000,
			Coin:     "ETH",
			Side:     "B",
			Px:       "2000",
			Sz:       "1",
			Fee:      "0.5",
			Tid:      888,
			Oid:      999,
			FeeToken: "", // intentionally missing — should cause FetchTrades to fail loudly
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(malformed)
	}))
	defer server.Close()

	c := &Client{
		apiURL:     server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	accountID := uuid.New()
	account := &models.ExchangeAccount{
		ID:                accountID.String(),
		AccountIdentifier: "0x1234567890abcdef1234567890abcdef12345678",
	}

	trades, _, err := c.FetchTrades(context.Background(), account, time.UnixMilli(1700000000000))
	if err == nil {
		t.Fatalf("expected error from FetchTrades for malformed fill, got trades=%v", trades)
	}
	if !strings.Contains(err.Error(), "feeToken") {
		t.Errorf("error = %q, want substring 'feeToken'", err.Error())
	}
}
