// Package client is an HTTP client for the SonarQube APIs.
//
// The endpoints of SonarQube Cloud and of SonarQube Server are not identical,
// so a client carries the product it talks to. The transport lives here and
// each group of endpoints lives in its own file beside it, which is where the
// Server variants go when they arrive.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Product is the SonarQube product an instance runs.
type Product string

const (
	// ProductCloud is SonarQube Cloud, for example https://sonarcloud.io.
	ProductCloud Product = "cloud"
	// ProductServer is a SonarQube Server instance.
	ProductServer Product = "server"
)

// CloudURL is the address of the public SonarQube Cloud instance.
const CloudURL = "https://sonarcloud.io"

const defaultTimeout = 30 * time.Second

// Config holds what a client needs to reach one instance.
type Config struct {
	// URL is the base address of the instance, without a trailing slash.
	URL string
	// Token authenticates every request.
	Token string
	// Product says which SonarQube product answers at URL.
	Product Product
	// HTTPClient replaces the default client. Tests use it; leave it nil
	// elsewhere.
	HTTPClient *http.Client
}

// Client talks to one SonarQube instance.
type Client struct {
	url     string
	token   string
	product Product
	http    *http.Client
}

// New builds a client from cfg.
func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		url:     strings.TrimRight(cfg.URL, "/"),
		token:   cfg.Token,
		product: cfg.Product,
		http:    httpClient,
	}
}

// URL returns the base address of the instance.
func (c *Client) URL() string {
	return c.url
}

// Product returns the product the instance runs.
func (c *Client) Product() Product {
	return c.product
}

// IsCloud reports whether the instance is SonarQube Cloud. A resource that
// exists only in Cloud tests this before it does anything.
func (c *Client) IsCloud() bool {
	return c.product == ProductCloud
}

// get calls a read action of the web service and decodes the answer into out.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	target := c.url + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	// The web service takes the token as the basic-auth user name, with an
	// empty password.
	req.SetBasicAuth(c.token, "")

	return c.do(req, out)
}

// do sends req and decodes a successful answer into out. Pass a nil out to
// discard the body.
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
