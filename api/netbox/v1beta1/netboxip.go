package v1beta1

import (
	"errors"
	"fmt"
	"net"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:deepcopy-gen=true

// NetBoxIP represents the IP address exported to NetBox.
type NetBoxIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NetBoxIPSpec `json:"spec"`
}

// NetBoxIPSpec defines the custom fields of the NetBoxIP resource.
type NetBoxIPSpec struct {
	Address     IP     `json:"address"`
	DNSName     string `json:"dnsName"`
	Tags        []Tag  `json:"tags,omitempty"`
	Description string `json:"description,omitempty"`
}

// Tag is a NetBox tag.
type Tag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// IP represents an IPv4 or IPv6 address.
type IP net.IP

// UnmarshalText implements the encoding.TextUnmarshaler interface for IP.
func (ip *IP) UnmarshalText(b []byte) error {
	parsedIP := net.ParseIP(string(b))
	if parsedIP == nil {
		// net.ParseIP returns nil if the string is not a valid representation of an IP address
		return errors.New("IP could not be parsed")
	}
	*ip = IP(parsedIP)
	return nil
}

// MarshalText implements the encoding.TextMarshaler interface for IP.
func (ip IP) MarshalText() ([]byte, error) {
	return net.IP(ip).MarshalText()
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:deepcopy-gen=true

// NetBoxIPList represents a list of custom NetBoxIP resources.
type NetBoxIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:",inline"`

	Items []NetBoxIP `json:"items"`
}

var (
	dnsLabelRegexp = "[a-zA-Z0-9][a-zA-Z0-9-]{0,62}"
	dnsNameRegexp  = fmt.Sprintf("^(%s\\.)*%s$", dnsLabelRegexp, dnsLabelRegexp)

	tagSlugRegexp = "^[-a-zA-Z0-9_]+$"
)

var tagSchema = &apiextensionsv1.JSONSchemaProps{
	Type: "object",
	Properties: map[string]apiextensionsv1.JSONSchemaProps{
		"name": apiextensionsv1.JSONSchemaProps{
			Type:      "string",
			MinLength: pointer.Int64(1),
			MaxLength: pointer.Int64(100),
		},
		"slug": apiextensionsv1.JSONSchemaProps{
			Type:      "string",
			MinLength: pointer.Int64(1),
			MaxLength: pointer.Int64(100),
			Pattern:   tagSlugRegexp,
		},
	},
}

// NetBoxIPValidationSchema is the validation schema for NetBoxIP resource.
var NetBoxIPValidationSchema = &apiextensionsv1.CustomResourceValidation{
	OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"spec": apiextensionsv1.JSONSchemaProps{Type: "object",
				Properties: map[string]apiextensionsv1.JSONSchemaProps{
					"address": apiextensionsv1.JSONSchemaProps{
						Type:      "string",
						MinLength: pointer.Int64(1),
						// instead of a very complex (for IPv6) regexp here,
						// we can determine whether the given string is a valid representation
						// of an IPv4/v6 address when parsing
					},
					"dnsName": apiextensionsv1.JSONSchemaProps{
						Type:      "string",
						MinLength: pointer.Int64(1),
						MaxLength: pointer.Int64(253),
						Pattern:   dnsNameRegexp,
					},
					"tags": apiextensionsv1.JSONSchemaProps{
						Type: "array",
						Items: &apiextensionsv1.JSONSchemaPropsOrArray{
							Schema: tagSchema,
						},
					},
					"description": apiextensionsv1.JSONSchemaProps{
						Type: "string",
						// limit set by NetBox
						MaxLength: pointer.Int64(200),
					},
				},
			},
		},
	},
}
