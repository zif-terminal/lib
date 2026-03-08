package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/machinebox/graphql"
)

// GraphQLClient is an interface for the GraphQL client (allows mocking for testing)
type GraphQLClient interface {
	Run(ctx context.Context, req *graphql.Request, resp interface{}) error
}

// graphqlClientAdapter wraps a concrete *graphql.Client to implement GraphQLClient interface
type graphqlClientAdapter struct {
	client *graphql.Client
}

func (a *graphqlClientAdapter) Run(ctx context.Context, req *graphql.Request, resp interface{}) error {
	return a.client.Run(ctx, req, resp)
}

// Client provides methods to interact with the database through Hasura GraphQL API
type Client struct {
	graphql GraphQLClient
	url     string
	secret  string
}

// ClientConfig holds configuration for creating a new Client
type ClientConfig struct {
	URL         string // Hasura GraphQL endpoint URL
	AdminSecret string // Hasura admin secret
}

// NewClient creates a new database client with a real GraphQL client
func NewClient(config ClientConfig) *Client {
	client := graphql.NewClient(config.URL)
	return &Client{
		graphql: &graphqlClientAdapter{client: client},
		url:     config.URL,
		secret:  config.AdminSecret,
	}
}

// NewClientWithGraphQL creates a client with a custom GraphQL client (for testing)
// This allows injecting a mock GraphQL client for unit tests
func NewClientWithGraphQL(graphql GraphQLClient, config ClientConfig) *Client {
	return &Client{
		graphql: graphql,
		url:     config.URL,
		secret:  config.AdminSecret,
	}
}

// graphqlRequest creates a new GraphQL request with admin secret header
func (c *Client) graphqlRequest(query string) *graphql.Request {
	req := graphql.NewRequest(query)
	req.Header.Set("X-Hasura-Admin-Secret", c.secret)
	return req
}

// graphqlRequestWithVars creates a new GraphQL request with variables
func (c *Client) graphqlRequestWithVars(query string, vars map[string]interface{}) *graphql.Request {
	req := c.graphqlRequest(query)
	for key, value := range vars {
		req.Var(key, value)
	}
	return req
}

// execute executes a GraphQL request and unmarshals the response
func (c *Client) execute(ctx context.Context, req *graphql.Request, resp interface{}) error {
	return c.graphql.Run(ctx, req, resp)
}

// DBClient is an interface that Client implements
type DBClient interface {
	// Exchange methods
	GetExchange(ctx context.Context, id string) (*Exchange, error)
	ListExchanges(ctx context.Context) ([]*Exchange, error)
	CreateExchange(ctx context.Context, input *ExchangeInput) (*Exchange, error)
	UpdateExchange(ctx context.Context, id string, input *ExchangeInput) (*Exchange, error)

	// Account methods
	GetAccount(ctx context.Context, id string) (*ExchangeAccount, error)
	ListAccounts(ctx context.Context) ([]*ExchangeAccount, error)
	CreateAccount(ctx context.Context, input *ExchangeAccountInput) (*ExchangeAccount, error)
	UpdateAccount(ctx context.Context, id string, input *ExchangeAccountInput) (*ExchangeAccount, error)
	DeleteAccount(ctx context.Context, id string) error

	// Trade methods
	GetTrade(ctx context.Context, id string) (*Trade, error)
	ListTrades(ctx context.Context, filter TradeFilter) ([]*Trade, error)
	CreateTrade(ctx context.Context, input *TradeInput) (*Trade, error)
	UpdateTrade(ctx context.Context, id string, input *TradeInput) (*Trade, error)
	DeleteTrade(ctx context.Context, id string) error
	LatestTrade(ctx context.Context, exchangeAccountIDs []uuid.UUID) (map[uuid.UUID]*Trade, error)

	// Funding payment methods
	GetLatestFundingPayment(ctx context.Context, exchangeAccountID uuid.UUID) (*FundingPayment, error)
	AddFundingPayments(ctx context.Context, inputs []*FundingPaymentInput) ([]*FundingPayment, error)
	ListFundingPayments(ctx context.Context, filter FundingPaymentFilter) ([]*FundingPayment, error)

	// Position methods
	DeletePositionsForAccount(ctx context.Context, accountID uuid.UUID) (int, error)
	AddPositions(ctx context.Context, inputs []*PositionInput) ([]*Position, error)
	AddPositionEvents(ctx context.Context, inputs []*PositionEventInput) (int, error)
	GetPositions(ctx context.Context, filter PositionFilter) ([]*Position, error)

	// Wallet methods
	GetWallet(ctx context.Context, id string) (*Wallet, error)
	GetWalletByAddress(ctx context.Context, address string, chain string) (*Wallet, error)
	ListWallets(ctx context.Context) ([]*Wallet, error)
	ListWalletsByChain(ctx context.Context, chain string) ([]*Wallet, error)
	CreateWallet(ctx context.Context, input *WalletInput) (*Wallet, error)
	DeleteWallet(ctx context.Context, id string) error
	UpdateWalletLastDetected(ctx context.Context, walletID string) error

	// Deposit methods
	GetLatestDeposit(ctx context.Context, exchangeAccountID uuid.UUID) (*Deposit, error)
	AddDeposits(ctx context.Context, inputs []*DepositInput) ([]*Deposit, error)
	UpdateDepositCostBasis(ctx context.Context, depositID uuid.UUID, userCostBasis string) (*Deposit, error)
	ListDeposits(ctx context.Context, filter DepositFilter) ([]*Deposit, error)

	// Transfer methods
	ListTransfers(ctx context.Context, filter TransferFilter) ([]*Transfer, error)
	AddTransfers(ctx context.Context, inputs []*TransferInput) ([]*Transfer, error)
	DeleteTransfersByAccountAndType(ctx context.Context, accountID uuid.UUID, transferType string) (int, error)

	// Settlement methods
	AddSettlements(ctx context.Context, inputs []*SettlementInput) (int, error)
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]*Settlement, error)
	GetLatestSettlement(ctx context.Context, exchangeAccountID uuid.UUID) (*Settlement, error)

	// Processor checkpoint methods
	SaveCheckpoint(ctx context.Context, accountID uuid.UUID, state json.RawMessage, lastEventTimestamp int64, eventCounts json.RawMessage) error
	LoadCheckpoint(ctx context.Context, accountID uuid.UUID) (*ProcessorCheckpoint, error)

	// Sync timestamp methods
	UpdateAccountLastSynced(ctx context.Context, accountID string) error
}

// Ensure Client implements DBClient
var _ DBClient = (*Client)(nil)
