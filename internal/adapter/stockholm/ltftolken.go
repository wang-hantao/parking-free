// Package stockholm contains the source adapter for Stockholm's
// LTF-Tolken parking-regulation API.
//
// See docs/04-stockholm-ltf-tolken-api.md for the full API reference.
// This package's job is to:
//  1. Speak HTTP to LTF-Tolken (this file).
//  2. Transform raw responses into domain.Regulation / domain.Rule
//     records (transform.go).
package stockholm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is an HTTP client for openparking.stockholm.se/LTF-Tolken/v1.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient constructs a Client. baseURL is typically
// "https://openparking.stockholm.se/LTF-Tolken/v1".
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Foreskrift identifies one of the six exposed regulation types.
type Foreskrift string

const (
	Servicedagar    Foreskrift = "servicedagar"    // street cleaning
	PTillaten       Foreskrift = "ptillaten"       // parking permitted
	PBuss           Foreskrift = "pbuss"           // bus parking only
	PLastbil        Foreskrift = "plastbil"        // truck parking only
	PMotorcykel     Foreskrift = "pmotorcykel"     // motorcycle parking only
	PRorelsehindrad Foreskrift = "prorelsehindrad" // disabled-driver parking only
)

// AllForeskrifter is the canonical list, useful for bulk-ingest loops.
var AllForeskrifter = []Foreskrift{
	Servicedagar, PTillaten, PBuss, PLastbil, PMotorcykel, PRorelsehindrad,
}

// FetchAll calls the /all endpoint for the given föreskrift and
// returns the raw JSON bytes. Callers pass these to Transform to
// produce domain records.
func (c *Client) FetchAll(ctx context.Context, f Foreskrift) ([]byte, error) {
	return c.get(ctx, fmt.Sprintf("/%s/all", f), nil)
}

// FetchWithin calls the /within endpoint for nearby rules.
func (c *Client) FetchWithin(ctx context.Context, f Foreskrift, lat, lng float64, radiusM int) ([]byte, error) {
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("lng", strconv.FormatFloat(lng, 'f', 6, 64))
	q.Set("radius", strconv.Itoa(radiusM))
	return c.get(ctx, fmt.Sprintf("/%s/within", f), q)
}

// FetchUntilNextWeekday calls the servicedagar /untilNextWeekday endpoint.
// Only applicable to Servicedagar.
func (c *Client) FetchUntilNextWeekday(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/servicedagar/untilNextWeekday", nil)
}

// get is the common HTTP plumbing. It sets the apiKey query parameter
// and outputFormat=json, and returns the response body.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if c.APIKey == "" {
		return nil, errors.New("stockholm: APIKey not configured (request one at https://openparking.stockholm.se/Home/Key)")
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("apiKey", c.APIKey)
	q.Set("outputFormat", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("stockholm: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stockholm: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("stockholm: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("stockholm: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	// Sanity-check that the body parses as JSON; LTF-Tolken can return
	// HTML error pages on bad keys.
	if !json.Valid(body) {
		return nil, fmt.Errorf("stockholm: response is not valid JSON (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
