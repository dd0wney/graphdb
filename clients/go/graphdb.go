package graphdb

import (
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Client is the top-level graphdb client.
type Client struct {
	t *transport

	Nodes  *Nodes
	Edges  *Edges
	Search *Search
}

// Option configures a Client.
type Option func(*config)

type config struct {
	token      string
	apiKey     string
	username   string
	password   string
	timeout    time.Duration
	retries    int
	httpClient *http.Client
	authModes  int
}

func WithToken(t string) Option  { return func(c *config) { c.token = t; c.authModes++ } }
func WithAPIKey(k string) Option { return func(c *config) { c.apiKey = k; c.authModes++ } }
func WithLogin(user, pass string) Option {
	return func(c *config) { c.username = user; c.password = pass; c.authModes++ }
}
func WithTimeout(d time.Duration) Option   { return func(c *config) { c.timeout = d } }
func WithRetries(n int) Option             { return func(c *config) { c.retries = n } }
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// New builds a Client. Exactly one auth mode (token, api key, or login) is
// required. baseURL must be an absolute http(s) URL; note that http sends
// credentials in cleartext and belongs to local development only.
func New(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, errors.New("graphdb: baseURL is required")
	}
	if u, err := url.Parse(baseURL); err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("graphdb: baseURL must be an absolute http(s) URL")
	}
	cfg := &config{timeout: 30 * time.Second, retries: 2}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.authModes != 1 {
		return nil, errors.New("graphdb: exactly one auth mode required (WithToken, WithAPIKey, or WithLogin)")
	}
	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.timeout}
	}
	t := &transport{
		baseURL:    baseURL,
		http:       hc,
		token:      cfg.token,
		apiKey:     cfg.apiKey,
		username:   cfg.username,
		password:   cfg.password,
		maxRetries: cfg.retries,
	}
	c := &Client{t: t}
	c.Nodes = &Nodes{t: t}
	c.Edges = &Edges{t: t}
	c.Search = &Search{t: t}
	return c, nil
}

// Facet types; methods are added in their own files.
type Nodes struct{ t *transport }
type Edges struct{ t *transport }
type Search struct{ t *transport }
