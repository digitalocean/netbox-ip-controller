package netbox

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCustomFieldUnmarshaling(t *testing.T) {
	tests := []struct {
		name                string
		data                string
		expectedCustomField *CustomField
		shouldError         bool
	}{{
		name:                "empty",
		data:                "{}",
		expectedCustomField: &CustomField{},
	}, {
		name: "with labeled fields as objects",
		data: `{
			"type": {
			  "value": "text",
			  "label": "Text"
			},
			"filter_logic": {
			  "value": "exact",
			  "label": "Exact"
			}
		}`,
		expectedCustomField: &CustomField{
			Type:        "text",
			FilterLogic: "exact",
		},
	}, {
		name: "with labeled fields as strings",
		data: `{
			"type": "text",
			"filter_logic": "exact"
		}`,
		expectedCustomField: &CustomField{
			Type:        "text",
			FilterLogic: "exact",
		},
	}, {
		name: "with labeled fields as unexpected values",
		data: `{
			"type": 123
		}`,
		shouldError: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var field CustomField
			err := json.Unmarshal([]byte(test.data), &field)
			if !test.shouldError && err != nil {
				t.Errorf("want no error, got %q\n", err)
			} else if test.shouldError && err == nil {
				t.Error("want an error, got nil")
			}

			if !test.shouldError {
				if diff := cmp.Diff(test.expectedCustomField, &field); diff != "" {
					t.Errorf("(-want, +got)\n%s", diff)
				}
			}
		})
	}
}

func TestIPAddressUnmarshaling(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		expectedIP  *IPAddress
		shouldError bool
	}{{
		name:       "empty",
		data:       "{}",
		expectedIP: &IPAddress{},
	}, {
		name: "with IPv4 address",
		data: `{
			"id": 123,
			"address": "192.168.0.1/32"
		}`,
		expectedIP: &IPAddress{
			ID:      123,
			Address: IP(net.IPv4(192, 168, 0, 1)),
		},
	}, {
		name: "with IPv6 address",
		data: `{
			"id": 123,
			"address": "1:2::3/128"
		}`,
		expectedIP: &IPAddress{
			ID:      123,
			Address: IP(net.IP{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
		},
	}, {
		name: "with invalid address",
		data: `{
			"id": 123,
			"address": "not-an-ip/1"
		}`,
		shouldError: true,
	}, {
		name: "with uid",
		data: `{
			"id": 123,
			"custom_fields": {
				"netbox_ip_controller_uid": "5d9b8cf3-feba-4d73-8075-18b99783b7be",
				"some_irrelevant_field": 123
			}
		}`,
		expectedIP: &IPAddress{
			ID:  123,
			UID: UID("5d9b8cf3-feba-4d73-8075-18b99783b7be"),
		},
	}, {
		name: "with custom fields but no uid",
		data: `{
			"id": 123,
			"custom_fields": {
				"some_irrelevant_field": 123
			}
		}`,
		expectedIP: &IPAddress{
			ID: 123,
		},
	}, {
		name: "with tags",
		data: `{
			"id": 123,
			"tags": [{
				"id": 5,
				"name": "foo",
				"slug": "bar"
			}]
		}`,
		expectedIP: &IPAddress{
			ID: 123,
			Tags: []Tag{{
				ID:   5,
				Name: "foo",
				Slug: "bar",
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ip IPAddress
			err := json.Unmarshal([]byte(test.data), &ip)
			if !test.shouldError && err != nil {
				t.Errorf("want no error, got %q\n", err)
			} else if test.shouldError && err == nil {
				t.Error("want an error, got nil")
			}

			if !test.shouldError {
				if diff := cmp.Diff(test.expectedIP, &ip); diff != "" {
					t.Errorf("(-want, +got)\n%s", diff)
				}
			}
		})
	}
}

func TestIPAddressMarshaling(t *testing.T) {
	tests := []struct {
		name         string
		ip           *IPAddress
		expectedData string
		shouldError  bool
	}{{
		name:         "empty",
		ip:           &IPAddress{},
		expectedData: `{}`,
	}, {
		name: "with IPv4 address",
		ip: &IPAddress{
			ID:      123,
			Address: IP(net.IPv4(192, 168, 0, 1)),
		},
		expectedData: `{
			"id": 123,
			"address": "192.168.0.1/32"
		}`,
	}, {
		name: "with IPv6 address",
		ip: &IPAddress{
			ID:      123,
			Address: IP(net.IP{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
		},
		expectedData: `{
			"id": 123,
			"address": "1:2::3/128"
		}`,
	}, {
		name: "with invalid address",
		ip: &IPAddress{
			ID:      123,
			Address: IP([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
		},
		shouldError: true,
	}, {
		name: "with uid",
		ip: &IPAddress{
			ID:  123,
			UID: UID("5d9b8cf3-feba-4d73-8075-18b99783b7be"),
		},
		expectedData: `{
			"id": 123,
			"custom_fields": {
				"netbox_ip_controller_uid": "5d9b8cf3-feba-4d73-8075-18b99783b7be"
			}
		}`,
	}, {
		name: "with tags",
		ip: &IPAddress{
			ID: 123,
			Tags: []Tag{{
				ID:   5,
				Name: "foo",
				Slug: "bar",
			}},
		},
		expectedData: `{
			"id": 123,
			"tags": [{
				"id": 5,
				"name": "foo",
				"slug": "bar"
			}]
		}`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var prefix, indent = "", "  "
			actualData, err := json.MarshalIndent(test.ip, prefix, indent)
			if !test.shouldError && err != nil {
				t.Errorf("want no error, got %q\n", err)
			} else if test.shouldError && err == nil {
				t.Error("want an error, got nil")
			}

			if !test.shouldError {
				diff, err := jsonDiff([]byte(test.expectedData), actualData)
				if err != nil {
					t.Fatalf("error comparing json: %s", err)
				}
				if diff != "" {
					t.Errorf("(-want, +got)\n%s", diff)
				}
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

// jsonDiff compares json ignoring the order of fields and spacing.
func jsonDiff(b1, b2 []byte) (string, error) {
	var o1, o2 map[string]interface{}
	if err := json.Unmarshal(b1, &o1); err != nil {
		return "", err
	}
	if err := json.Unmarshal(b2, &o2); err != nil {
		return "", err
	}

	formattedB1, err := json.MarshalIndent(o1, "", "  ")
	if err != nil {
		return "", err
	}
	formattedB2, err := json.MarshalIndent(o2, "", "  ")
	if err != nil {
		return "", err
	}

	return cmp.Diff(formattedB1, formattedB2), nil
}
