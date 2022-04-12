package netbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// CustomField is a NetBox custom field attached to some model(s).
type CustomField struct {
	ID              int64  `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	Label           string `json:"label,omitempty"`
	Description     string `json:"description,omitempty"`
	Required        bool   `json:"required,omitempty"`
	ValidationRegex string `json:"validation_regex,omitempty"`
	// Type is the type of the field.
	// Possible values: text, longtext, integer, boolean, date, url, json, select, multiselect
	Type LabeledString `json:"type"`
	// ContentTypes is the list of modelt to which the custom field is added.
	// Should be in format "domain.object", e.g. "ipam.ipaddress".
	ContentTypes []string `json:"content_types"`
	// FilterLogic can be one of: disabled, loose, exact. Specified how the field
	// will be matched when persorming a query.
	FilterLogic LabeledString `json:"filter_logic,omitempty"`
	// Weight is for display purposes: fields with higher weights appear lower in a form.
	Weight int64 `json:"weight,omitempty"`
}

// LabeledString represents the kind of field in NetBox which is a string
// upon writing to NetBox, but is an object {"value": "string", "label": "string"},
// upon retrieving from NetBox.
type LabeledString string

// UnmarshalJSON implements the json.Unmarshaler interface for LabeledString.
func (v *LabeledString) UnmarshalJSON(b []byte) error {
	var obj interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("unmarshaling labeled string: %w", err)
	}

	switch ot := obj.(type) {
	case string:
		*v = LabeledString(ot)
	case map[string]interface{}:
		val, ok := ot["value"]
		if !ok {
			return errors.New("cannot unmarshal labeled string: \"value\" is missing")
		}
		if s, ok := val.(string); ok {
			*v = LabeledString(s)
		} else {
			return errors.New("cannot unmarshal labeled string: \"value\" is not a string")
		}
	default:
		return errors.New("cannot unmarshal labeled string: neither a string nor a map[string]string")
	}

	return nil
}

// CustomFieldList represents the response from the NetBox endpoints that return
// multiple custom fields.
type CustomFieldList struct {
	Count   uint          `json:"count"`
	Results []CustomField `json:"results"`
}

// Tag represents a NetBox tag.
type Tag struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Slug string `json:"slug,omitempty"`
}

// TagList represents the response from the NetBox endpoints that return multiple tags.
type TagList struct {
	Count   uint  `json:"count"`
	Results []Tag `json:"results"`
}

// IPAddress represents a NetBox IP address.
type IPAddress struct {
	ID int64 `json:"id,omitempty"`
	// UID is the UID of the object that this IP is assigned to.
	// It is stored in NetBox as a custom field.
	UID     UID    `json:"custom_fields,omitempty"`
	DNSName string `json:"dns_name,omitempty"`
	// TODO(dasha): in go 1.18, there's a new net/netip package with
	// a better (immutable and comparable) netip.Addr
	Address     IP     `json:"address,omitempty"`
	Tags        []Tag  `json:"tags,omitempty"`
	Description string `json:"description,omitempty"`
}

// IPAddressList represents the response from the NetBox endpoints that return multiple IP addresses.
type IPAddressList struct {
	Count   uint        `json:"count"`
	Results []IPAddress `json:"results"`
}

// UID is the type for representing UID of an IPAddress.
// Its purpose is to provide custom marshaling and unmarshaling.
type UID string

// UnmarshalJSON implements the json.Unmarshaler interface for UID.
func (uid *UID) UnmarshalJSON(b []byte) error {
	var customFields map[string]interface{}
	if err := json.Unmarshal(b, &customFields); err != nil {
		return fmt.Errorf("unmarshaling UID from custom fields: %w", err)
	}

	if u, ok := customFields[UIDCustomFieldName].(string); ok {
		*uid = UID(u)
	}
	// if there's no UID present, that's not an error
	return nil
}

// MarshalJSON implements the json.Marshaler interface for UID.
func (uid UID) MarshalJSON() ([]byte, error) {
	customFields := make(map[string]string)
	customFields[UIDCustomFieldName] = string(uid)
	return json.Marshal(customFields)
}

// IP is the type for representing address from NetBox.
// Its purpose is to provide custom marshaling and unmarshaling.
type IP net.IP

// UnmarshalJSON implements the json.Unmarshaler interface for IP.
func (ip *IP) UnmarshalJSON(b []byte) error {
	var addrStr string
	if err := json.Unmarshal(b, &addrStr); err != nil {
		return fmt.Errorf("unmarshaling address to string: %w", err)
	}
	addr, _, err := net.ParseCIDR(addrStr)
	if err != nil {
		return fmt.Errorf("parsing address: %w", err)
	}
	*ip = IP(addr)
	return nil
}

// MarshalText implements the encoding.TextMarshaler interface for IP.
func (ip IP) MarshalText() ([]byte, error) {
	var cidrSuffix string

	// net.IP.To4() returns nil if the address is not an IPv4 address,
	// and net.IP.To16() - if not a valid IPv6
	isValidIPv4 := (net.IP(ip).To4() != nil)
	isValidIPv6 := (net.IP(ip).To16() != nil)

	if isValidIPv4 && isValidIPv6 {
		cidrSuffix = "32"
	} else if isValidIPv6 && !isValidIPv4 {
		cidrSuffix = "128"
	} else {
		return nil, fmt.Errorf("%q is not a valid IPv4 or IPv6 address", ip)
	}

	return []byte(fmt.Sprintf("%s/%s", net.IP(ip).String(), cidrSuffix)), nil
}

func (ip *IPAddress) changed(ip2 *IPAddress) bool {
	if ip == nil && ip2 == nil {
		return false
	} else if ip == nil || ip2 == nil {
		// one is nil and the other one isn't
		return true
	}

	// slug names are required to be unique, so can base sorting on it
	sortTags := func(t1, t2 Tag) bool { return t1.Name < t2.Name }

	return !cmp.Equal(ip, ip2,
		cmpopts.SortSlices(sortTags),
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreUnexported(IPAddress{}, Tag{}),
	)
}
