package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client reads the mihomo REST API (external-controller).
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, secret string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http: &http.Client{
			Timeout: timeout,
			// The API is local to the pod; never send it through a proxy
			// picked up from the environment.
			Transport: &http.Transport{Proxy: nil},
		},
	}
}

type versionResponse struct {
	Version string `json:"version"`
}

type providersResponse struct {
	Providers map[string]provider `json:"providers"`
}

type provider struct {
	Name             string            `json:"name"`
	VehicleType      string            `json:"vehicleType"`
	TestURL          string            `json:"testUrl"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	SubscriptionInfo *subscriptionInfo `json:"subscriptionInfo"`
	Proxies          []proxy           `json:"proxies"`
}

// subscriptionInfo is the parsed `subscription-userinfo` header of the last
// successful subscription download.
type subscriptionInfo struct {
	Upload   int64 `json:"Upload"`
	Download int64 `json:"Download"`
	Total    int64 `json:"Total"`
	Expire   int64 `json:"Expire"`
}

type proxiesResponse struct {
	Proxies map[string]proxy `json:"proxies"`
}

// proxy is a node or a group as the API reports it. Groups have All.
type proxy struct {
	Name    string                `json:"name"`
	Type    string                `json:"type"`
	Alive   bool                  `json:"alive"`
	History []delayRecord         `json:"history"`
	Extra   map[string]proxyState `json:"extra"`
	All     []string              `json:"all"`
	Now     string                `json:"now"`
	TestURL string                `json:"testUrl"`
}

// proxyState is the health of a node for one test URL.
type proxyState struct {
	Alive   bool          `json:"alive"`
	History []delayRecord `json:"history"`
}

type delayRecord struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

// nonNodeTypes are built-in outbounds and groups: they are not servers of a
// subscription and are left out of node metrics.
var nonNodeTypes = map[string]bool{
	"Direct": true, "Reject": true, "RejectDrop": true, "Compatible": true,
	"Pass": true, "PassRule": true, "Dns": true,
	"Selector": true, "Fallback": true, "URLTest": true, "LoadBalance": true,
	"Relay": true, "Smart": true,
}

func (p proxy) isNode() bool {
	return p.All == nil && !nonNodeTypes[p.Type]
}

// aliveFor reports whether the node passed its last check against url. A node
// that has never been checked against url falls back to its last check of any
// URL.
func (p proxy) aliveFor(url string) bool {
	if url != "" {
		if s, ok := p.Extra[url]; ok {
			return s.Alive
		}
	}
	return p.Alive
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: decode: %w", path, err)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var r versionResponse
	err := c.get(ctx, "/version", &r)
	return r.Version, err
}

func (c *Client) Providers(ctx context.Context) (map[string]provider, error) {
	var r providersResponse
	err := c.get(ctx, "/providers/proxies", &r)
	return r.Providers, err
}

func (c *Client) Proxies(ctx context.Context) (map[string]proxy, error) {
	var r proxiesResponse
	err := c.get(ctx, "/proxies", &r)
	return r.Proxies, err
}
