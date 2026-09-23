// Package client is an HTTP client for the SonarQube APIs.
//
// SonarQube Cloud and SonarQube Server do not offer the same endpoints, so a
// client carries the product it talks to.
//
// Two API surfaces answer at two hosts. Web API v2 answers at the api host,
// takes a bearer token and reports a missing entity with a 404. The older web
// service answers at the instance host, takes the token as the basic-auth
// user name, and is the only surface that can create or delete an
// organization.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Product is the SonarQube product an instance runs.
type Product string

// The SonarQube products this client can talk to.
const (
	ProductCloud  Product = "cloud"
	ProductServer Product = "server"
)

// CloudURL is the address of the public SonarQube Cloud instance.
const CloudURL = "https://sonarcloud.io"

const defaultTimeout = 30 * time.Second

// Config configures a Client.
type Config struct {
	URL string
	// APIURL is the address of Web API v2. An empty value is derived from URL
	// with DeriveAPIURL.
	APIURL  string
	Token   string
	Product Product
	// HTTPClient replaces the default client. Tests set it; leave it nil
	// elsewhere.
	HTTPClient *http.Client
}

// Client talks to one SonarQube instance.
type Client struct {
	url     string
	apiURL  string
	token   string
	product Product
	http    *http.Client
}

// New builds a Client from cfg.
func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	url := strings.TrimRight(cfg.URL, "/")

	apiURL := strings.TrimRight(cfg.APIURL, "/")
	if apiURL == "" {
		apiURL = DeriveAPIURL(url)
	}

	return &Client{
		url:     url,
		apiURL:  apiURL,
		token:   cfg.Token,
		product: cfg.Product,
		http:    httpClient,
	}
}

// DeriveAPIURL returns the address of Web API v2 that belongs to a web
// application address.
//
//	https://sonarcloud.io      -> https://api.sonarcloud.io
//	https://dev11.sc-dev11.io  -> https://api.sc-dev11.io
//
// The api host sits beside the web application rather than below it, so a host
// that already carries a sub-domain has that sub-domain replaced. An address
// that names a host by number is returned unchanged, because nothing can be
// derived from it.
func DeriveAPIURL(instanceURL string) string {
	parsed, err := url.Parse(instanceURL)
	if err != nil || parsed.Host == "" {
		return instanceURL
	}

	hostname := parsed.Hostname()
	if net.ParseIP(hostname) != nil {
		return instanceURL
	}

	// Read the port before the host is rewritten.
	port := parsed.Port()

	labels := strings.Split(hostname, ".")
	if len(labels) > 2 {
		labels[0] = "api"
	} else {
		labels = append([]string{"api"}, labels...)
	}

	parsed.Host = strings.Join(labels, ".")
	if port != "" {
		parsed.Host += ":" + port
	}
	return strings.TrimRight(parsed.String(), "/")
}

// URL returns the base address of the instance.
func (c *Client) URL() string {
	return c.url
}

// APIURL returns the address of Web API v2.
func (c *Client) APIURL() string {
	return c.apiURL
}

// Product returns the product the instance runs.
func (c *Client) Product() Product {
	return c.product
}

// IsCloud reports whether the instance is SonarQube Cloud.
func (c *Client) IsCloud() bool {
	return c.product == ProductCloud
}

// get calls the older web service.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	target := c.url + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	// The web service takes the token as the basic-auth user name.
	req.SetBasicAuth(c.token, "")

	return c.do(req, out)
}

// apiGet calls Web API v2, which takes a bearer token.
func (c *Client) apiGet(ctx context.Context, path string, params url.Values, out any) error {
	target := c.apiURL + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	return c.do(req, out)
}

// do decodes a successful answer into out. A nil out discards the body.
func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read the response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp.StatusCode, body)
	}

	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("cannot decode the response body: %w", err)
	}
	return nil
}
