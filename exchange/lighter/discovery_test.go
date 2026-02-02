package lighter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_DiscoverAccounts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "/api/v1/accountsByL1Address", r.URL.Path)
		assert.Equal(t, "0xabc123def456", r.URL.Query().Get("l1_address"))
		assert.Equal(t, "ro:0:all:9999999999:abc123", r.Header.Get("Authorization"))

		json.NewEncoder(w).Encode(lighterAccountsResponse{
			Code:      200,
			L1Address: "0xabc123def456",
			SubAccounts: []lighterAccount{
				{
					Index:       0,
					L1Address:   "0xabc123def456",
					AccountType: 0, // main
					Collateral:  "100.5",
				},
				{
					Index:       1,
					L1Address:   "0xabc123def456",
					AccountType: 1, // sub
					Collateral:  "50.0",
				},
				{
					Index:       2,
					L1Address:   "0xabc123def456",
					AccountType: 1, // sub
					Collateral:  "25.0",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	userIdentifier := "0xabc123def456:ro:0:all:9999999999:abc123"
	accounts, err := client.DiscoverAccounts(context.Background(), userIdentifier)
	require.NoError(t, err)
	assert.Len(t, accounts, 3)

	// Verify main account
	assert.Equal(t, "0", accounts[0].AccountIdentifier)
	assert.Equal(t, "main", accounts[0].AccountType)
	assert.Equal(t, "Main Account", accounts[0].Name)
	assert.Equal(t, "0xabc123def456", accounts[0].Metadata["l1_address"])
	assert.Equal(t, "ro:0:all:9999999999:abc123", accounts[0].Metadata["auth_token"])

	// Verify first sub-account
	assert.Equal(t, "1", accounts[1].AccountIdentifier)
	assert.Equal(t, "sub_account", accounts[1].AccountType)
	assert.Equal(t, "Sub-Account 1", accounts[1].Name)

	// Verify second sub-account
	assert.Equal(t, "2", accounts[2].AccountIdentifier)
	assert.Equal(t, "sub_account", accounts[2].AccountType)
	assert.Equal(t, "Sub-Account 2", accounts[2].Name)
}

func TestClient_DiscoverAccounts_SingleAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lighterAccountsResponse{
			Code:      200,
			L1Address: "0xtest",
			SubAccounts: []lighterAccount{
				{
					Index:       156333,
					L1Address:   "0xtest",
					AccountType: 0, // main
					Collateral:  "0.009136",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	accounts, err := client.DiscoverAccounts(context.Background(), "0xtest:ro:0:single:9999999999:xyz")
	require.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "156333", accounts[0].AccountIdentifier)
	assert.Equal(t, "main", accounts[0].AccountType)
	assert.Equal(t, "Main Account", accounts[0].Name)
}

func TestClient_DiscoverAccounts_InvalidUserIdentifier(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name           string
		userIdentifier string
		expectedError  string
	}{
		{
			name:           "no colon separator",
			userIdentifier: "0xabc123",
			expectedError:  "invalid userIdentifier format",
		},
		{
			name:           "empty l1 address",
			userIdentifier: ":ro:0:all:999:abc",
			expectedError:  "l1_address is required",
		},
		{
			name:           "invalid auth token format",
			userIdentifier: "0xabc:invalid_token",
			expectedError:  "invalid auth token format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.DiscoverAccounts(context.Background(), tt.userIdentifier)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestClient_DiscoverAccounts_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(lighterAccountsResponse{})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.DiscoverAccounts(ctx, "0xtest:ro:0:all:999:abc")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestClient_DiscoverAccounts_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	_, err := client.DiscoverAccounts(context.Background(), "0xtest:ro:0:all:999:abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestClient_DiscoverAccounts_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	_, err := client.DiscoverAccounts(context.Background(), "0xtest:ro:0:all:999:abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestClient_DiscoverAccounts_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lighterAccountsResponse{
			Code:        200,
			L1Address:   "0xtest",
			SubAccounts: []lighterAccount{},
		})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	accounts, err := client.DiscoverAccounts(context.Background(), "0xtest:ro:0:all:999:abc")
	require.NoError(t, err)
	assert.Len(t, accounts, 0)
}

func TestClient_DiscoverAccounts_MetadataPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lighterAccountsResponse{
			Code:      200,
			L1Address: "0xmyaddr",
			SubAccounts: []lighterAccount{
				{Index: 5, L1Address: "0xmyaddr", AccountType: 1, Collateral: "10"},
			},
		})
	}))
	defer server.Close()

	client := NewClient()
	client.baseURL = server.URL

	l1Addr := "0xmyaddr"
	authToken := "ro:5:single:1234567890:deadbeef"
	userIdentifier := l1Addr + ":" + authToken

	accounts, err := client.DiscoverAccounts(context.Background(), userIdentifier)
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	// Verify metadata is correctly set
	assert.Equal(t, l1Addr, accounts[0].Metadata["l1_address"])
	assert.Equal(t, authToken, accounts[0].Metadata["auth_token"])
}
