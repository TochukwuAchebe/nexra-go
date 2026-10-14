// Package nexra is the Go SDK for the Nexra platform API.
//
// Every operation in the API is generated from its OpenAPI document into the
// nexraapi package. This package adds what the generator does not: client
// construction from a token and workspace, retries with backoff, typed errors,
// cursor pages, safe run starts and webhook signature checks.
//
//	client, err := nexra.NewClient(nexra.Options{}) // NEXRA_TOKEN and NEXRA_WORKSPACE_ID
//	integrations, _, err := client.IntegrationsAPI.ListIntegrations(ctx).Execute()
package nexra

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/TochukwuAchebe/nexra-go/nexraapi"
)

// Version is this SDK's version.
const Version = "1.0.0"

// APIVersion is the dated version of the Nexra API this SDK was built from.
const APIVersion = "2026-10-01"

// DefaultBaseURL is the Nexra API.
const DefaultBaseURL = "https://api.nexra.amakoragroup.com"

// Options configure a Client. Every field is optional.
type Options struct {
	// Token is a Nexra access token (sent as Authorization: Bearer).
	// Defaults to the NEXRA_TOKEN environment variable.
	Token string
	// TokenSource returns a current token for each request, for tokens you
	// refresh yourself. It takes precedence over Token.
	TokenSource func() (string, error)
	// WorkspaceID is the workspace to act in (X-Workspace-Id).
	// Defaults to NEXRA_WORKSPACE_ID.
	WorkspaceID string
	// BaseURL defaults to NEXRA_API_URL, then DefaultBaseURL.
	BaseURL string
	// Retry sets backoff. Nil uses DefaultRetryOptions.
	Retry *RetryOptions
	// DisableRetries turns retries off.
	DisableRetries bool
	// HTTPClient is used for requests. Its Transport is wrapped with auth
	// and retries. Defaults to a client with a 30 second timeout.
	HTTPClient *http.Client
	// Headers are added to every request.
	Headers map[string]string
}

// Client is the generated API client with the SDK's auth and retries
// installed. Use its service fields, for example client.RunsAPI.
type Client struct {
	*nexraapi.APIClient
	// BaseURL is the API address the client calls.
	BaseURL string
}

// ErrNoToken is returned by NewClient when no token is given or set.
var ErrNoToken = errors.New("nexra: an access token is needed: set Options.Token or NEXRA_TOKEN")

// NewClient makes a client for the Nexra API.
func NewClient(options Options) (*Client, error) {
	token := options.Token
	if token == "" {
		token = os.Getenv("NEXRA_TOKEN")
	}
	if token == "" && options.TokenSource == nil {
		return nil, ErrNoToken
	}
	workspace := options.WorkspaceID
	if workspace == "" {
		workspace = os.Getenv("NEXRA_WORKSPACE_ID")
	}
	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("NEXRA_API_URL")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	if options.HTTPClient != nil {
		copied := *options.HTTPClient
		httpClient = &copied
	}
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	retry := DefaultRetryOptions
	if options.Retry != nil {
		retry = *options.Retry
	}
	if options.DisableRetries {
		retry.MaxRetries = 0
	}
	source := options.TokenSource
	if source == nil {
		source = func() (string, error) { return token, nil }
	}
	httpClient.Transport = &authTransport{
		base:      &RetryTransport{Base: base, Options: retry},
		token:     source,
		workspace: workspace,
		headers:   options.Headers,
	}

	config := nexraapi.NewConfiguration()
	config.Servers = nexraapi.ServerConfigurations{{URL: baseURL, Description: "Nexra"}}
	config.OperationServers = map[string]nexraapi.ServerConfigurations{}
	config.UserAgent = "nexra-sdk-go/" + Version
	config.HTTPClient = httpClient
	return &Client{APIClient: nexraapi.NewAPIClient(config), BaseURL: baseURL}, nil
}

type authTransport struct {
	base      http.RoundTripper
	token     func() (string, error)
	workspace string
	headers   map[string]string
}

func (t *authTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	token, err := t.token()
	if err != nil {
		return nil, err
	}
	request = request.Clone(request.Context())
	for name, value := range t.headers {
		request.Header.Set(name, value)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if t.workspace != "" {
		request.Header.Set("X-Workspace-Id", t.workspace)
	}
	request.Header.Set("X-Nexra-Client", "nexra-sdk-go/"+Version)
	return t.base.RoundTrip(request)
}
