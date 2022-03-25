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
		ips:  ips,
	}
}

// GetTag returns a tag with the given name from fake NetBox.
func (c *fakeClient) GetTag(_ context.Context, tag string) (*Tag, error) {
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

// GetIP returns an IP with the given UID and DNS name from fake NetBox.
func (c *fakeClient) GetIP(_ context.Context, key IPAddressKey) (*IPAddress, error) {
	if ip, ok := c.ips[key.UID]; ok && ip.DNSName == key.DNSName {
		return &ip, nil
	}
	return nil, nil
}

// UpsertIP adds an IP to fake NetBox or updates it if already exists.
func (c *fakeClient) UpsertIP(_ context.Context, ip *IPAddress) (*IPAddress, error) {
	if c.ips == nil {
		c.ips = make(map[string]IPAddress)
	}
	c.ips[ip.UID] = *ip
	return ip, nil
}

// DeleteIP deletes an IP with the given UID and DNS name from fake NetBox.
func (c *fakeClient) DeleteIP(_ context.Context, key IPAddressKey) error {
	if ip, ok := c.ips[key.UID]; ok && ip.DNSName == key.DNSName {
		delete(c.ips, key.UID)
	}
	return nil
}

// CreateUIDField is a noop.
func (c *fakeClient) CreateUIDField(ctx context.Context) error {
	return nil
}
