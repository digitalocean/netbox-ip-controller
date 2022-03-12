package netbox

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/netbox-community/go-netbox/netbox/models"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/pointer"
)

const uidRegexpStr = "^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$"

var (
	tagSlugRegexp = regexp.MustCompile("^[-a-zA-Z0-9_]+$")
	uidRegexp     = regexp.MustCompile(uidRegexpStr)
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

func (tag *Tag) validate() error {
	var result multierror.Error

	if tag == nil {
		multierror.Append(&result, errors.New("tag cannot be nil"))
		return result.ErrorOrNil()
	}
	if len(tag.Name) < 1 || len(tag.Name) > 100 {
		multierror.Append(&result, errors.New("name must be between 1 and 100 characters long"))
	}
	if len(tag.Slug) < 1 || len(tag.Slug) > 100 {
		multierror.Append(&result, errors.New("slug must be between 1 and 100 characters long"))
	}
	if !tagSlugRegexp.MatchString(tag.Slug) {
		multierror.Append(&result, errors.New("slug must contain only alphanumeric characters, '-', or '_'"))
	}

	return result.ErrorOrNil()
}

// IPAddress represents a NetBox IP address.
type IPAddress struct {
	id int64
	// UID is the UID of the object that this IP is assigned to.
	UID         string
	DNSName     string
	Address     net.IP
	Tags        []Tag
	Description string
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
		addr := strings.TrimSuffix(*netboxIP.Address, "/32")
		ip.Address = net.ParseIP(addr)
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

func (ip *IPAddress) toNetBox() *models.WritableIPAddress {
	if ip == nil {
		return nil
	}

	netboxIP := &models.WritableIPAddress{
		Description: ip.Description,
		DNSName:     ip.DNSName,
		// nil tags are not allowed by NetBox's validation
		Tags: []*models.NestedTag{},
		Role: "vip",
	}

	if ip.Address != nil {
		// TODO(dasha): "/32" only works for IPv4 - need to handle IPv6 as well
		netboxIP.Address = pointer.String(fmt.Sprintf("%s/32", ip.Address.String()))
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

	return netboxIP
}

func (ip *IPAddress) validate() error {
	var result multierror.Error

	if ip == nil {
		multierror.Append(&result, errors.New("IP address cannot be nil"))
		return result.ErrorOrNil()
	}
	if !uidRegexp.MatchString(ip.UID) {
		multierror.Append(&result, errors.New("UID must be a valid uuid string"))
	}
	if errs := validation.IsDNS1123Subdomain(ip.DNSName); errs != nil {
		for _, err := range errs {
			multierror.Append(&result, errors.New(err))
		}
	}
	if ip.Address == nil || ip.Address.IsUnspecified() {
		multierror.Append(&result, errors.New("address must be specified"))
	}
	if len(ip.Description) > 200 {
		multierror.Append(&result, errors.New("description max length is 200 characters"))
	}
	for _, tag := range ip.Tags {
		if err := tag.validate(); err != nil {
			multierror.Append(&result, fmt.Errorf("tag %s is invalid: %w", tag.Name, err))
		}
	}

	return result.ErrorOrNil()
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
