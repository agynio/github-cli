package reviewapi

import (
	"context"
	"net/http"

	"github.com/cli/cli/v2/api"
)

const (
	restAcceptHeaderValue = "application/vnd.github+json"
	restAPIVersionHeader  = "X-GitHub-Api-Version"
	restAPIVersionValue   = "2022-11-28"
)

// Service provides access to GitHub REST and GraphQL APIs needed for review management commands.
type Service struct {
	rest *RestClient
	gql  *api.Client
	host string
}

// NewService constructs a Service using the provided HTTP client and hostname.
func NewService(httpClient *http.Client, host string) *Service {
	restClient := NewRestClient(httpClient, host)
	gqlClient := api.NewClientFromHTTP(httpClient)

	return &Service{
		rest: restClient,
		gql:  gqlClient,
		host: host,
	}
}

// Host returns the configured API hostname.
func (s *Service) Host() string {
	return s.host
}

// Rest exposes the REST client wrapper.
func (s *Service) Rest() *RestClient {
	return s.rest
}

// GraphQL exposes the GraphQL client.
func (s *Service) GraphQL() *api.Client {
	return s.gql
}

// CurrentLogin returns the login name for the authenticated user.
func (s *Service) CurrentLogin(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}

	_, err := s.rest.GetJSON(ctx, "/user", nil, &user)
	if err != nil {
		return "", err
	}

	return user.Login, nil
}
