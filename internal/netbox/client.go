package netbox

// A wrapper that abstracts out some go-netbox client details,
// and makes talking to netbox a bit nicer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	httptransport "github.com/go-openapi/runtime/client"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	netbox "github.com/netbox-community/go-netbox/netbox/client"
	"github.com/netbox-community/go-netbox/netbox/client/extras"
	"github.com/netbox-community/go-netbox/netbox/client/ipam"
	"github.com/netbox-community/go-netbox/netbox/models"
	log "go.uber.org/zap"
	"k8s.io/utils/pointer"
)

// UIDCustomFieldName is the name of the custom field in NetBox,
// containing the UID of the resource that an IP is assigned to.
const UIDCustomFieldName = "netbox_ip_controller__uid"

// Client is a netbox client.
type Client interface {
	GetTagByName(ctx context.Context, tag string) (*Tag, error)
	CreateTag(ctx context.Context, tag string) (*Tag, error)
	GetIP(ctx context.Context, key IPAddressKey) (*IPAddress, error)
	UpsertIP(ctx context.Context, ip *IPAddress) (*IPAddress, error)
	DeleteIP(ctx context.Context, key IPAddressKey) error
	CreateUIDField(ctx context.Context) error
}

type client netbox.NetBoxAPI

// NewClient sets up a new NetBox client with default authorization
// and retries.
func NewClient(apiURL, apiToken string) (Client, error) {
	u, err := parseAndValidateURL(apiURL)
	if err != nil {
		return nil, err
	}

	transport := httptransport.NewWithClient(
		u.Host,
		u.Path,
		[]string{u.Scheme},
		retryableHTTPClient(5),
	)
	transport.DefaultAuthentication = httptransport.APIKeyAuth("Authorization", "header", "Token "+apiToken)

	c := client(*netbox.New(transport, nil))

	return &c, nil
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

func retryableHTTPClient(retryMax int) *http.Client {
	// add retries on 50X errors
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = retryMax
	retryClient.CheckRetry = func(ctx context.Context, res *http.Response, err error) (bool, error) {
		if err == nil {
			// do not retry non-idempotent requests
			method := res.Request.Method
			if method == http.MethodPost || method == http.MethodPatch {
				return false, nil
			}
		}
		return retryablehttp.DefaultRetryPolicy(ctx, res, err)
	}

	return retryClient.StandardClient()
}

// CreateUIDField adds a custom field with name UIDCustomFieldName
// to NetBox IPAddresses.
func (c *client) CreateUIDField(ctx context.Context) error {
	params := extras.NewExtrasCustomFieldsCreateParamsWithContext(ctx)
	params.SetData(&models.WritableCustomField{
		ContentTypes:    []string{"ipam.ipaddress"},
		Description:     "UID of the object the IP is assigned to.",
		FilterLogic:     "exact",
		Label:           "UID",
		Name:            pointer.String(UIDCustomFieldName),
		Required:        false,
		Type:            "text",
		ValidationRegex: uidRegexpStr,
		Weight:          pointer.Int64(100),
	})

	if _, err := c.Extras.ExtrasCustomFieldsCreate(params, nil); err != nil {
		return fmt.Errorf("cannot create custom UID field: %w", err)
	}

	return nil
}

// GetTagByName returns a tag with the given name.
func (c *client) GetTagByName(ctx context.Context, tag string) (*Tag, error) {
	params := extras.NewExtrasTagsListParamsWithContext(ctx)
	params.SetName(pointer.String(tag))

	res, err := c.Extras.ExtrasTagsList(params, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}

	tags := res.GetPayload().Results
	if len(tags) > 1 {
		// netbox tag names must be unique, so this should never happen
		return nil, fmt.Errorf("more than one tag with name %q found", tag)
	}
	if len(tags) == 0 {
		return nil, nil
	}

	return tagFromNetBox(tags[0]), nil
}

// CreateTag creates a tag with the given name. Tag slug is set to the
// same value as tag name.
func (c *client) CreateTag(ctx context.Context, tag string) (*Tag, error) {
	t := &Tag{
		Name: tag,
		Slug: tag,
	}

	// validation errors returned by go-netbox client don't give any details
	// beyond the 400 status code; so, do our own validation to provide
	// better error messages
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("validating tag: %w", err)
	}

	params := extras.NewExtrasTagsCreateParamsWithContext(ctx)
	params.SetData(t.toNetBox())

	res, err := c.Extras.ExtrasTagsCreate(params, nil)
	if err != nil {
		return nil, fmt.Errorf("creting tag: %w", err)
	}

	return tagFromNetBox(res.GetPayload()), nil
}

// GetIP returns an IP address with the given UID and DNS name.
func (c *client) GetIP(ctx context.Context, key IPAddressKey) (*IPAddress, error) {
	params := ipam.NewIpamIPAddressesListParamsWithContext(ctx)
	params.SetDNSName(&key.DNSName)
	var limit int64 = 100
	params.SetLimit(pointer.Int64(limit))

	// technically, there can be an unlimited number of IPs with the given DNSName;
	// even though realistically that shouldn't be the case, we still iterate over
	// multiple pages of results if necessary
	var offset int64
	for {
		params.SetOffset(pointer.Int64(offset))
		res, err := c.Ipam.IpamIPAddressesList(params, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot lookup IPs: %w", err)
		}

		for _, netboxIP := range res.GetPayload().Results {
			ip := ipAddressFromNetBox(netboxIP)
			if ip.UID == key.UID {
				return ip, nil
			}
		}

		if res.GetPayload().Next == nil {
			// we've reached the last page, nothing more to fetch
			break
		}

		offset = offset + limit
	}

	return nil, nil
}

// UpsertIP creates an IP address or updates one, if an IP with the same
// UID and DNS name already exists.
func (c *client) UpsertIP(ctx context.Context, ip *IPAddress) (*IPAddress, error) {
	// validation errors returned by go-netbox client don't give any details
	// beyond the 400 status code; so, do our own validation to provide
	// better error messages
	if err := ip.validate(); err != nil {
		return nil, fmt.Errorf("validating IP: %w", err)
	}

	existingIP, err := c.GetIP(ctx, IPAddressKey{UID: ip.UID, DNSName: ip.DNSName})
	if err != nil {
		return nil, err
	}

	if existingIP != nil && !existingIP.changed(ip) {
		log.L().Info("IP has not changed - not updating")
		return nil, nil
	}

	if existingIP != nil {
		params := ipam.NewIpamIPAddressesUpdateParamsWithContext(ctx)
		params.SetID(existingIP.id)
		params.SetData(ip.toNetBox())

		res, err := c.Ipam.IpamIPAddressesUpdate(params, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot update IP: %w", err)
		}

		return ipAddressFromNetBox(res.GetPayload()), nil
	}

	params := ipam.NewIpamIPAddressesCreateParamsWithContext(ctx)
	params.SetData(ip.toNetBox())

	res, err := c.Ipam.IpamIPAddressesCreate(params, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create IP: %w", err)
	}

	return ipAddressFromNetBox(res.GetPayload()), nil
}

// DeleteIP deletes an IP with the given UID and DNS name from NetBox.
func (c *client) DeleteIP(ctx context.Context, key IPAddressKey) error {
	ip, err := c.GetIP(ctx, key)
	if err != nil {
		return err
	}

	if ip == nil {
		return nil
	}

	params := ipam.NewIpamIPAddressesDeleteParamsWithContext(ctx)
	params.SetID(ip.id)

	if _, err := c.Ipam.IpamIPAddressesDelete(params, nil); err != nil {
		return fmt.Errorf("cannot delete IP: %w", err)
	}
	return nil
}
