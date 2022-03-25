package netbox

import (
	"fmt"
	"net"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/netbox-community/go-netbox/netbox/models"
	"k8s.io/utils/pointer"
)

// Tag represents a NetBox tag.
type Tag struct {
	id   int64
	Name string
	Slug string
}

func tagFromNetBox(netboxTag *models.Tag) *Tag {
	if netboxTag == nil {
		return nil
	}

	tag := &Tag{id: netboxTag.ID}
	if netboxTag.Name != nil {
		tag.Name = *netboxTag.Name
	}
	if netboxTag.Slug != nil {
		tag.Slug = *netboxTag.Slug
	}

	return tag
}

func tagFromNetBoxNestedTag(nt *models.NestedTag) *Tag {
	if nt == nil {
		return nil
	}

	return tagFromNetBox(&models.Tag{
		ID:   nt.ID,
		Name: nt.Name,
		Slug: nt.Slug,
	})
}

func (tag *Tag) toNetBox() *models.Tag {
	if tag == nil {
		return nil
	}

	return &models.Tag{
		ID:   tag.id,
		Name: pointer.String(tag.Name),
		Slug: pointer.String(tag.Slug),
	}
}

// IPAddress represents a NetBox IP address.
type IPAddress struct {
	id int64
	// UID is the UID of the object that this IP is assigned to.
	UID     string
	DNSName string
	// TODO(dasha): in go 1.18, there's a new net/netip package with
	// a better (immutable and comparable) netip.Addr
	Address     net.IP
	Tags        []Tag
	Description string
}

// IPAddressKey is used to uniquely identify an IP address.
type IPAddressKey struct {
	UID     string
	DNSName string
}

func ipAddressFromNetBox(netboxIP *models.IPAddress) *IPAddress {
	if netboxIP == nil {
		return nil
	}

	ip := &IPAddress{
		id:          netboxIP.ID,
		DNSName:     netboxIP.DNSName,
		Description: netboxIP.Description,
	}

	if netboxIP.Address != nil {
		addr, _, err := net.ParseCIDR(*netboxIP.Address)
		if err == nil {
			ip.Address = addr
		}
	}

	if customFields, ok := netboxIP.CustomFields.(map[string]interface{}); ok {
		if uid, ok := customFields[UIDCustomFieldName].(string); ok {
			ip.UID = uid
		}
	}

	for _, netboxTag := range netboxIP.Tags {
		tag := tagFromNetBoxNestedTag(netboxTag)
		if tag != nil {
			ip.Tags = append(ip.Tags, *tag)
		}
	}

	return ip
}

func (ip *IPAddress) toNetBox() (*models.WritableIPAddress, error) {
	if ip == nil {
		return nil, nil
	}

	netboxIP := &models.WritableIPAddress{
		Description: ip.Description,
		DNSName:     ip.DNSName,
		// nil tags are not allowed by NetBox's validation
		Tags: []*models.NestedTag{},
		Role: "vip",
	}

	if ip.Address != nil {
		var cidrSuffix string

		// net.IP.To4() returns nil if the address is not an IPv4 address,
		// and net.IP.To16() - if not a valid IPv6
		isValidIPv4 := (ip.Address.To4() != nil)
		isValidIPv6 := (ip.Address.To16() != nil)

		if isValidIPv4 && isValidIPv6 {
			cidrSuffix = "32"
		} else if !isValidIPv4 {
			cidrSuffix = "128"
		} else {
			return nil, fmt.Errorf("%q is not a valid IPv4 or IPv6 address", ip.Address)
		}

		netboxIP.Address = pointer.String(fmt.Sprintf("%s/%s", ip.Address.String(), cidrSuffix))
	}

	if ip.UID != "" {
		netboxIP.CustomFields = map[string]string{UIDCustomFieldName: ip.UID}
	}

	for _, tag := range ip.Tags {
		netboxIP.Tags = append(netboxIP.Tags, &models.NestedTag{
			Name: pointer.String(tag.Name),
			Slug: pointer.String(tag.Slug),
		})
	}

	return netboxIP, nil
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
