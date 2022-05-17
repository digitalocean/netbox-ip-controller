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

import (
	"context"
	"errors"
)

type fakeClient struct {
	tags map[string]Tag
	ips  map[UID]IPAddress
}

// NewFakeClient returns a fake NetBox client.
func NewFakeClient(tags map[string]Tag, ips map[UID]IPAddress) Client {
	if tags == nil {
		tags = make(map[string]Tag)
	}
	if ips == nil {
		ips = make(map[UID]IPAddress)
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

// GetIP returns an IP with the given UID from fake NetBox.
func (c *fakeClient) GetIP(_ context.Context, uid UID) (*IPAddress, error) {
	if ip, ok := c.ips[uid]; ok {
		return &ip, nil
	}
	return nil, nil
}

// UpsertIP adds an IP to fake NetBox or updates it if already exists.
func (c *fakeClient) UpsertIP(_ context.Context, ip *IPAddress) (*IPAddress, error) {
	if c.ips == nil {
		c.ips = make(map[UID]IPAddress)
	}
	c.ips[ip.UID] = *ip
	return ip, nil
}

// DeleteIP deletes an IP with the given UID from fake NetBox.
func (c *fakeClient) DeleteIP(_ context.Context, uid UID) error {
	delete(c.ips, uid)
	return nil
}

// UpsertUIDField is a noop.
func (c *fakeClient) UpsertUIDField(ctx context.Context) error {
	return nil
}
