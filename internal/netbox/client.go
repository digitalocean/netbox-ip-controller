/*
Copyright 2022 DigitalOcean

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at:

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package netbox

// A wrapper that abstracts out some go-netbox client details,
// and makes talking to netbox a bit nicer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/digitalocean/netbox-ip-controller/internal/metrics"

	"github.com/hashicorp/go-cleanhttp"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	log "go.uber.org/zap"
	"golang.org/x/time/rate"
)

const (
	// UIDCustomFieldName is the name of the custom field in NetBox,
	// containing the UID of the resource that an IP is assigned to.
	UIDCustomFieldName = "netbox_ip_controller_uid"
	uidRegexpStr       = "^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$"

	// max size of response body that we ever expect to get, in bytes:
	// a safeguard in case we get a never-ending or extremely long response
	responseBodySizeLimit = 1 << 20
)

// Client is a netbox client.
type Client interface {
	GetTag(ctx context.Context, tag string) (*Tag, error)
	CreateTag(ctx context.Context, tag string) (*Tag, error)
	GetIP(ctx context.Context, uid UID) (*IPAddress, error)
	UpsertIP(ctx context.Context, ip *IPAddress) (*IPAddress, error)
	DeleteIP(ctx context.Context, uid UID) error
	UpsertUIDField(ctx context.Context) error
}

type client struct {
	httpClient  *retryablehttp.Client
	baseURL     string
	token       string
	rateLimiter *rate.Limiter
	logger      *log.Logger
}

// ClientOption is a function type to pass options to NewClient
type ClientOption func(*client) error

// NewClient sets up a new NetBox client with default authorization
// and retries.
func NewClient(apiURL, apiToken string, opts ...ClientOption) (Client, error) {
	u, err := parseAndValidateURL(apiURL)
	if err != nil {
		return nil, err
	}

	c := &client{
		httpClient: retryablehttp.NewClient(),
		baseURL:    strings.TrimSuffix(u.String(), "/"),
		token:      apiToken,
		logger:     log.L(),
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	c.httpClient.RetryMax = 5
	c.httpClient.Logger = newRetryableHTTPLogger(c.logger)

	if c.rateLimiter == nil {
		c.rateLimiter = rate.NewLimiter(rate.Inf, 1)
	}

	return c, nil
}

// WithLogger sets the logger to be used by the client.
func WithLogger(logger *log.Logger) ClientOption {
	return func(c *client) error {
		c.logger = logger
		return nil
	}
}

// WithRateLimiter is a functional option that attaches a token bucket style rate limiter
// to the given client.
func WithRateLimiter(refillRate rate.Limit, bucketSize int) ClientOption {
	return func(c *client) error {
		c.rateLimiter = rate.NewLimiter(refillRate, bucketSize)
		return nil
	}
}

// WithCARootCert is a functional option that adds the PEM-encoded root certificate
// found at the given path to the TLSClientConfig of the client's underlying HTTPClient.
func WithCARootCert(path string) ClientOption {
	return func(c *client) error {
		cert, err := os.ReadFile(path)
		if err != nil {
			if c.logger != nil {
				c.logger.Error(err.Error())
			} else {
				log.L().Error(err.Error())
			}
			return err
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(cert) {
			return errors.New("no certificates were successfully parsed")
		}
		// Use cleanhttp.DefaultTransport, as that's what is used by retryablehttp.NewClient()
		transport := cleanhttp.DefaultTransport()
		transport.TLSClientConfig = &tls.Config{
			RootCAs: certPool,
		}
		c.httpClient.HTTPClient.Transport = transport
		return nil
	}
}

func parseAndValidateURL(apiURL string) (*url.URL, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NetBox URL: %w", err)
	} else if !u.IsAbs() || u.Hostname() == "" {
		return nil, errors.New("NetBox URL must be in scheme://host:port format")
	}
	return u, nil
}

// NOTE: trailing "/" is required for endpoints that work with a single object ID
// (e.g. PUT /someobj/1/, DELETE /someobj/1/): without it, NetBox will always return
// 200 without actually making any changes ¯\_(ツ)_/¯

// UpsertUIDField adds a custom field with name UIDCustomFieldName
// to NetBox IPAddresses if it doesn't exist.
func (c *client) UpsertUIDField(ctx context.Context) error {
	existingField, err := c.getCustomUIDField(ctx)
	if err != nil {
		return fmt.Errorf("checking for existing UID field: %w", err)
	}

	if existingField != nil {
		c.logger.Info("UID field already exists")
		return nil
	}

	url := fmt.Sprintf("%s/extras/custom-fields/", c.baseURL)

	field := CustomField{
		ContentTypes:    []string{"ipam.ipaddress"},
		Description:     "UID of the object the IP is assigned to.",
		FilterLogic:     "exact",
		Label:           "UID",
		Name:            UIDCustomFieldName,
		Required:        false,
		Type:            "text",
		ValidationRegex: uidRegexpStr,
		Weight:          100,
	}

	if _, err := c.executeRequest(ctx, url, http.MethodPost, field); err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	return nil
}

func (c *client) getCustomUIDField(ctx context.Context) (*CustomField, error) {
	url := fmt.Sprintf("%s/extras/custom-fields/?name=%s", c.baseURL, UIDCustomFieldName)

	data, err := c.executeRequest(ctx, url, http.MethodGet, nil)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var fieldList CustomFieldList
	if err := json.Unmarshal(data, &fieldList); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(fieldList.Results) > 1 {
		// should never happen since names of custom fields must be unique
		return nil, fmt.Errorf("more than one custom field %q found", UIDCustomFieldName)
	}
	if len(fieldList.Results) == 0 {
		return nil, nil
	}

	return &fieldList.Results[0], nil
}

// GetTag returns a tag with the given name.
func (c *client) GetTag(ctx context.Context, tag string) (*Tag, error) {
	url := fmt.Sprintf("%s/extras/tags/?name=%s", c.baseURL, tag)

	data, err := c.executeRequest(ctx, url, http.MethodGet, nil)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var tagList TagList
	if err := json.Unmarshal(data, &tagList); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(tagList.Results) > 1 {
		// netbox tag names must be unique, so this should never happen
		return nil, fmt.Errorf("more than one tag with name %q found", tag)
	}
	if len(tagList.Results) == 0 {
		return nil, nil
	}

	return &tagList.Results[0], nil
}

// CreateTag creates a tag with the given name. Tag slug is set to the
// same value as tag name.
func (c *client) CreateTag(ctx context.Context, tag string) (*Tag, error) {
	url := fmt.Sprintf("%s/extras/tags/", c.baseURL)

	t := &Tag{
		Name: tag,
		Slug: tag,
	}
	data, err := c.executeRequest(ctx, url, http.MethodPost, t)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var createdTag Tag
	if err := json.Unmarshal(data, &createdTag); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &createdTag, nil
}

// GetIP returns an IP address with the given ID.
func (c *client) GetIP(ctx context.Context, uid UID) (*IPAddress, error) {
	url := fmt.Sprintf("%s/ipam/ip-addresses/?cf_%s=%s", c.baseURL, UIDCustomFieldName, uid)

	data, err := c.executeRequest(ctx, url, http.MethodGet, nil)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var ipList IPAddressList
	if err := json.Unmarshal(data, &ipList); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(ipList.Results) > 1 {
		// may happen either when a duplicate is accidentally created,
		// or if the UID custom field hasn't been created (in this case
		// NetBox won't do any filtering at all)
		return nil, fmt.Errorf("more than one IP with UID %q found", uid)
	}
	if len(ipList.Results) == 0 {
		return nil, nil
	}

	return &ipList.Results[0], nil
}

// UpsertIP creates an IP address or updates one, if an IP with the same
// UID already exists.
func (c *client) UpsertIP(ctx context.Context, ip *IPAddress) (*IPAddress, error) {
	existingIP, err := c.GetIP(ctx, ip.UID)
	if err != nil {
		return nil, fmt.Errorf("checking for existing IP: %w", err)
	}

	if existingIP != nil && !existingIP.changed(ip) {
		c.logger.Info("IP has not changed - not updating")
		return nil, nil
	}

	var data []byte
	if existingIP != nil {
		url := fmt.Sprintf("%s/ipam/ip-addresses/%d/", c.baseURL, existingIP.ID)
		data, err = c.executeRequest(ctx, url, http.MethodPut, ip)
	} else {
		url := fmt.Sprintf("%s/ipam/ip-addresses/", c.baseURL)
		data, err = c.executeRequest(ctx, url, http.MethodPost, ip)
	}
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var createdIP IPAddress
	if err := json.Unmarshal(data, &createdIP); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &createdIP, nil
}

// DeleteIP deletes an IP with the given UID from NetBox.
func (c *client) DeleteIP(ctx context.Context, uid UID) error {
	existingIP, err := c.GetIP(ctx, uid)
	if err != nil {
		return fmt.Errorf("checking if IP exists: %w", err)
	}

	if existingIP == nil {
		return nil
	}

	url := fmt.Sprintf("%s/ipam/ip-addresses/%d/", c.baseURL, existingIP.ID)
	if _, err := c.executeRequest(ctx, url, http.MethodDelete, nil); err != nil {
		return fmt.Errorf("executing request: %w", err)
	}

	return nil
}

func (c *client) executeRequest(ctx context.Context, url string, method string, body interface{}) ([]byte, error) {
	var b []byte
	var err error
	if body != nil {
		if b, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("marshaling body: %w", err)
		}
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req = req.WithContext(ctx)

	req.Header.Set("Accept", "application/json")
	if b != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Token %s", c.token))
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	var res *http.Response
	var responseErr error
	if method == http.MethodPost || method == http.MethodPatch {
		// non-idempotent method - we should not retry it
		res, responseErr = c.httpClient.HTTPClient.Do(req)
	} else {
		retryableReq, err := retryablehttp.FromRequest(req)
		if err != nil {
			return nil, err
		}
		res, responseErr = c.httpClient.Do(retryableReq)

	}
	if responseErr != nil {
		metrics.IncrementNetboxRequests(false)
		return nil, responseErr
	}
	defer res.Body.Close()

	if err := httpErrorFrom(res); err != nil {
		metrics.IncrementNetboxRequests(false)
		return nil, err
	}

	metrics.IncrementNetboxRequests(true)

	data, err := io.ReadAll(io.LimitReader(res.Body, responseBodySizeLimit))
	if err != nil {
		return nil, errors.New("reading response data")
	}
	return data, err
}

func httpErrorFrom(res *http.Response) error {
	if c := res.StatusCode; 200 <= c && c <= 299 {
		return nil
	}

	data, err := io.ReadAll(io.LimitReader(res.Body, responseBodySizeLimit))
	if err != nil {
		return fmt.Errorf("read error response data: %w", err)
	}
	if len(data) > 0 {
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	return errors.New(res.Status)
}
