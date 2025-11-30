package reviewapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghinstance"
)

var (
	retrySchedule = []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}
)

// RestResponse captures the outcome of a REST request.
type RestResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// RestClient issues REST requests with standardized headers and retry handling.
type RestClient struct {
	client *http.Client
	host   string
}

// NewRestClient creates a RestClient bound to the provided HTTP client and GitHub host.
func NewRestClient(client *http.Client, host string) *RestClient {
	return &RestClient{client: client, host: host}
}

// GetJSON performs a GET request and unmarshals the response body into dest.
func (c *RestClient) GetJSON(ctx context.Context, path string, query url.Values, dest interface{}) (*RestResponse, error) {
	return c.doJSON(ctx, http.MethodGet, path, query, nil, dest)
}

// PostJSON performs a POST request and unmarshals the response body into dest.
func (c *RestClient) PostJSON(ctx context.Context, path string, query url.Values, body interface{}, dest interface{}) (*RestResponse, error) {
	return c.doJSON(ctx, http.MethodPost, path, query, body, dest)
}

// PutJSON performs a PUT request and unmarshals the response body into dest.
func (c *RestClient) PutJSON(ctx context.Context, path string, query url.Values, body interface{}, dest interface{}) (*RestResponse, error) {
	return c.doJSON(ctx, http.MethodPut, path, query, body, dest)
}

func (c *RestClient) doJSON(ctx context.Context, method string, path string, query url.Values, body interface{}, dest interface{}) (*RestResponse, error) {
	var payload []byte
	switch v := body.(type) {
	case nil:
	case []byte:
		payload = v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}

	resp, err := c.do(ctx, method, path, query, payload)
	if err != nil {
		return nil, err
	}

	if dest == nil || len(resp.Body) == 0 {
		return resp, nil
	}

	if err := json.Unmarshal(resp.Body, dest); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *RestClient) do(ctx context.Context, method string, path string, query url.Values, body []byte) (*RestResponse, error) {
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, err := c.buildRequest(ctx, method, path, query, body)
		if err != nil {
			return nil, err
		}

		resp, err := c.client.Do(req)
		if err != nil {
			if attempt < len(retrySchedule) && isRetryableNetErr(err) {
				if sleepErr := sleep(ctx, retrySchedule[attempt]); sleepErr != nil {
					return nil, sleepErr
				}
				attempt++
				continue
			}
			return nil, err
		}

		if shouldRetryStatus(resp.StatusCode) {
			if attempt < len(retrySchedule) {
				delay := nextDelay(resp.Header, retrySchedule[attempt])
				resp.Body.Close()
				if sleepErr := sleep(ctx, delay); sleepErr != nil {
					return nil, sleepErr
				}
				attempt++
				continue
			}

			err = api.HandleHTTPError(resp)
			resp.Body.Close()
			return nil, err
		}

		if resp.StatusCode >= 400 {
			err = api.HandleHTTPError(resp)
			resp.Body.Close()
			return nil, err
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		return &RestResponse{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       bodyBytes,
		}, nil
	}
}

func (c *RestClient) buildRequest(ctx context.Context, method string, path string, query url.Values, body []byte) (*http.Request, error) {
	endpoint := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		endpoint = ghinstance.RESTPrefix(c.host) + strings.TrimPrefix(path, "/")
	}

	if len(query) > 0 {
		q := query.Encode()
		if strings.Contains(endpoint, "?") {
			endpoint += "&" + q
		} else {
			endpoint += "?" + q
		}
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", restAcceptHeaderValue)
	req.Header.Set(restAPIVersionHeader, restAPIVersionValue)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func shouldRetryStatus(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code < 600
}

func isRetryableNetErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

func nextDelay(headers http.Header, fallback time.Duration) time.Duration {
	retryAfter := headers.Get("Retry-After")
	if retryAfter == "" {
		return fallback
	}

	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		d := time.Duration(seconds) * time.Second
		if d > 0 {
			return d
		}
	}

	if ts, err := http.ParseTime(retryAfter); err == nil {
		if until := time.Until(ts); until > 0 {
			return until
		}
	}

	return fallback
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NextLink extracts the "next" relation from a Link header if present.
func NextLink(header string) (string, bool) {
	if header == "" {
		return "", false
	}

	parts := strings.Split(header, ",")
	for _, part := range parts {
		section := strings.TrimSpace(part)
		if !strings.HasSuffix(section, "rel=\"next\"") {
			continue
		}

		start := strings.Index(section, "<")
		end := strings.Index(section, ">")
		if start == -1 || end == -1 || end <= start+1 {
			continue
		}

		target := section[start+1 : end]
		u, err := url.Parse(target)
		if err != nil {
			return target, true
		}

		if u.Scheme != "" && u.Host != "" {
			result := u.Path
			if u.RawQuery != "" {
				result += "?" + u.RawQuery
			}
			return result, true
		}

		return target, true
	}

	return "", false
}
