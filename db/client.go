package db

import (
	"context"

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
}

// Ensure Client implements DBClient
var _ DBClient = (*Client)(nil)
