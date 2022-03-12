package netbox

import (
	"context"
	"errors"
)

type fakeClient struct {
	tags map[string]Tag
	ips  map[string]IPAddress
}

// NewFakeClient returns a fake NetBox client.
func NewFakeClient(tags map[string]Tag, ips map[string]IPAddress) Client {
	if tags == nil {
		tags = make(map[string]Tag)
	}
	if ips == nil {
		ips = make(map[string]IPAddress)
	}
	return &fakeClient{
		tags: tags,
	}
}

// GetTagByName returns a tag with the given name from fake NetBox.
func (c *fakeClient) GetTagByName(_ context.Context, tag string) (*Tag, error) {
	if t, ok := c.tags[tag]; ok {
		return &t, nil
	}
	return nil, nil
}

// CreateTag adds a tag with the given name to fake NetBox.
func (c *fakeClient) CreateTag(_ context.Context, tag string) (*Tag, error) {
	if _, ok := c.tags[tag]; ok {
		return nil, errors.New("tag already exists")
	}
	t := Tag{
		Name: tag,
		Slug: tag,
	}
	c.tags[tag] = t
	return &t, nil
}

// GetIPByUID returns an IP with the given uid and DNS name from fake NetBox.
func (c *fakeClient) GetIPByUID(_ context.Context, uid, dnsName string) (*IPAddress, error) {
	if ip, ok := c.ips[uid]; ok && ip.DNSName == dnsName {
		return &ip, nil
	}
	return nil, nil
}

// UpsertIP adds an IP to fake NetBox or updates it if already exists.
func (c *fakeClient) UpsertIP(_ context.Context, ip *IPAddress) (*IPAddress, error) {
	c.ips[ip.UID] = *ip
	return ip, nil
}

// CreateUIDField is a noop.
func (c *fakeClient) CreateUIDField(ctx context.Context) error {
	return nil
}
