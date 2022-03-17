package netbox

import (
	"net"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/netbox-community/go-netbox/netbox/models"
	"k8s.io/utils/pointer"
)

func TestTagFromNetBox(t *testing.T) {
	tests := []struct {
		name      string
		netboxTag *models.Tag
		tag       *Tag
	}{{
		name:      "nil",
		netboxTag: nil,
		tag:       nil,
	}, {
		name:      "with nil slug",
		netboxTag: &models.Tag{Name: pointer.String("name")},
		tag:       &Tag{Name: "name"},
	}, {
		name: "with name and slug",
		netboxTag: &models.Tag{
			Name: pointer.String("name"),
			Slug: pointer.String("slug"),
		},
		tag: &Tag{Name: "name", Slug: "slug"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			convertedTag := tagFromNetBox(test.netboxTag)
			if diff := cmp.Diff(test.tag, convertedTag, cmpopts.IgnoreUnexported(Tag{})); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}

func TestTagFromNetBoxNestedTag(t *testing.T) {
	tests := []struct {
		name      string
		netboxTag *models.NestedTag
		tag       *Tag
	}{{
		name:      "nil",
		netboxTag: nil,
		tag:       nil,
	}, {
		name: "with name and slug",
		netboxTag: &models.NestedTag{
			Name: pointer.String("name"),
			Slug: pointer.String("slug"),
		},
		tag: &Tag{Name: "name", Slug: "slug"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			convertedTag := tagFromNetBoxNestedTag(test.netboxTag)
			if diff := cmp.Diff(test.tag, convertedTag, cmpopts.IgnoreUnexported(Tag{})); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}

func TestTagValidate(t *testing.T) {
	tests := []struct {
		name  string
		tag   *Tag
		valid bool
	}{{
		name:  "nil",
		tag:   nil,
		valid: false,
	}, {
		name: "name too long",
		tag: &Tag{
			Name: strings.Repeat("toolong", 20),
			Slug: "good",
		},
		valid: false,
	}, {
		name: "slug has disallowed characters",
		tag: &Tag{
			Name: "good",
			Slug: "bad!",
		},
		valid: false,
	}, {
		name: "valid",
		tag: &Tag{
			Name: "good",
			Slug: "also-good",
		},
		valid: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.tag.validate()
			if err != nil && test.valid {
				t.Errorf("want no validation error, got %q\n", err)
			} else if err == nil && !test.valid {
				t.Error("want validation error, nil")
			}
		})
	}
}

func TestIPAddressFromNetBox(t *testing.T) {
	tests := []struct {
		name     string
		netboxIP *models.IPAddress
		ip       *IPAddress
	}{{
		name:     "nil",
		netboxIP: nil,
		ip:       nil,
	}, {
		name:     "with IPv4 address",
		netboxIP: &models.IPAddress{Address: pointer.String("192.168.0.1/32")},
		ip:       &IPAddress{Address: net.IPv4(192, 168, 0, 1)},
	}, {
		name: "with uid",
		netboxIP: &models.IPAddress{
			CustomFields: map[string]interface{}{
				UIDCustomFieldName: "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			},
		},
		ip: &IPAddress{UID: "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3"},
	}, {
		name: "with tag",
		netboxIP: &models.IPAddress{
			Tags: []*models.NestedTag{{
				Name: pointer.String("name"),
				Slug: pointer.String("slug"),
			}},
		},
		ip: &IPAddress{
			Tags: []Tag{{Name: "name", Slug: "slug"}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			convertedIP := ipAddressFromNetBox(test.netboxIP)
			if diff := cmp.Diff(test.ip, convertedIP, cmpopts.IgnoreUnexported(IPAddress{}, Tag{})); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}

func TestIPAddressToNetBox(t *testing.T) {
	tests := []struct {
		name     string
		netboxIP *models.WritableIPAddress
		ip       *IPAddress
	}{{
		name:     "nil",
		netboxIP: nil,
		ip:       nil,
	}, {
		name: "with IPv4 address",
		netboxIP: &models.WritableIPAddress{
			Address: pointer.String("192.168.0.1/32"),
			Tags:    []*models.NestedTag{},
			Role:    "vip",
		},
		ip: &IPAddress{Address: net.IPv4(192, 168, 0, 1)},
	}, {
		name: "with uid",
		netboxIP: &models.WritableIPAddress{
			CustomFields: map[string]string{
				UIDCustomFieldName: "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			},
			Tags: []*models.NestedTag{},
			Role: "vip",
		},
		ip: &IPAddress{UID: "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3"},
	}, {
		name: "with tag",
		netboxIP: &models.WritableIPAddress{
			Tags: []*models.NestedTag{{
				Name: pointer.String("name"),
				Slug: pointer.String("slug"),
			}},
			Role: "vip",
		},
		ip: &IPAddress{
			Tags: []Tag{{Name: "name", Slug: "slug"}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			convertedIP := test.ip.toNetBox()
			if diff := cmp.Diff(test.netboxIP, convertedIP, cmpopts.IgnoreUnexported(IPAddress{}, Tag{})); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}

func TestIPAddressValidate(t *testing.T) {
	tests := []struct {
		name  string
		ip    *IPAddress
		valid bool
	}{{
		name:  "nil",
		ip:    nil,
		valid: false,
	}, {
		name: "missing address",
		ip: &IPAddress{
			UID:     "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			DNSName: "someapp-abc123.blah.blah",
		},
		valid: false,
	}, {
		name: "invalid uid",
		ip: &IPAddress{
			UID:     "not-a-uuid",
			Address: net.IPv4(192, 168, 0, 1),
			DNSName: "someapp-abc123.blah.blah",
		},
		valid: false,
	}, {
		name: "invalid dns name",
		ip: &IPAddress{
			DNSName: "_invalid._dns",
			Address: net.IPv4(192, 168, 0, 1),
			UID:     "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
		},
		valid: false,
	}, {
		name: "description too long",
		ip: &IPAddress{
			DNSName:     "someapp-abc123.blah.blah",
			Address:     net.IPv4(192, 168, 0, 1),
			UID:         "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			Description: strings.Repeat("toolong", 30),
		},
		valid: false,
	}, {
		name: "tag with missing name",
		ip: &IPAddress{
			DNSName: "someapp-abc123.blah.blah",
			Address: net.IPv4(192, 168, 0, 1),
			UID:     "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			Tags:    []Tag{{Slug: "slug"}},
		},
		valid: false,
	}, {
		name: "valid",
		ip: &IPAddress{
			Address:     net.IPv4(192, 168, 0, 1),
			UID:         "c9a5c3d1-8af4-4429-82a6-fcbb73f026f3",
			DNSName:     "someapp-abc123.blah.blah",
			Tags:        []Tag{{Name: "name", Slug: "slug"}},
			Description: "app: someapp",
		},
		valid: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.ip.validate()
			if err != nil && test.valid {
				t.Errorf("want no validation error, got %q\n", err)
			} else if err == nil && !test.valid {
				t.Error("want validation error, nil")
			}
		})
	}
}

func TestIPChanged(t *testing.T) {
	tests := []struct {
		name    string
		ip1     *IPAddress
		ip2     *IPAddress
		changed bool
	}{{
		name:    "both nil",
		ip1:     nil,
		ip2:     nil,
		changed: false,
	}, {
		name:    "only one is nil",
		ip1:     nil,
		ip2:     &IPAddress{},
		changed: true,
	}, {
		name: "with tags in different order",
		ip1: &IPAddress{
			Tags: []Tag{{Name: "tag1"}, {Name: "tag2"}},
		},
		ip2: &IPAddress{
			Tags: []Tag{{Name: "tag2"}, {Name: "tag1"}},
		},
		changed: false,
	}, {
		name: "with empty tags vs nil tags",
		ip1: &IPAddress{
			Tags: []Tag{},
		},
		ip2:     &IPAddress{},
		changed: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := test.ip1.changed(test.ip2)
			if changed != test.changed {
				t.Errorf("want ip.changed() = %t, got %t\n", test.changed, changed)
			}
		})
	}
}