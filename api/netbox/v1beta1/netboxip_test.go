package v1beta1

import (
	"net"
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIPUnmarshalText(t *testing.T) {
	tests := []struct {
		name  string
		ipStr string
		ip    net.IP
		valid bool
	}{{
		name:  "empty",
		ipStr: "",
		ip:    nil,
		valid: false,
	}, {
		name:  "valid IPv4",
		ipStr: "192.168.0.1",
		ip:    net.IPv4(192, 168, 0, 1),
		valid: true,
	}, {
		name:  "valid IPv6",
		ipStr: "1:2::3",
		ip:    net.IP{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3},
		valid: true,
	}, {
		name:  "invalid",
		ipStr: "not-an-ip",
		ip:    nil,
		valid: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ip IP
			err := ip.UnmarshalText([]byte(test.ipStr))
			if err != nil && test.valid {
				t.Errorf("want no error, got %q\n", err)
			} else if err == nil && !test.valid {
				t.Error("want error, nil")
			}

			if !test.ip.Equal(net.IP(ip)) {
				t.Errorf("want %v, got %v\n", test.ip, net.IP(ip))
			}
		})
	}
}

func TestValidationSchema(t *testing.T) {
	var schema apiextensions.CustomResourceValidation
	if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(NetBoxIPValidationSchema, &schema, nil); err != nil {
		t.Errorf("converting CRD validation: %q\n", err)
	}
	validator, _, err := apiservervalidation.NewSchemaValidator(&schema)
	if err != nil {
		t.Errorf("creating validator: %q\n", err)
	}

	tests := []struct {
		name         string
		netboxIPSpec NetBoxIPSpec
		valid        bool
	}{{
		name:         "empty",
		netboxIPSpec: NetBoxIPSpec{},
		valid:        false,
	}, {
		name:         "missing address",
		netboxIPSpec: NetBoxIPSpec{DNSName: "test"},
		valid:        false,
	}, {
		name: "invalid dns name",
		netboxIPSpec: NetBoxIPSpec{
			Address: IP(net.IPv4(8, 8, 8, 8)),
			DNSName: "!?not.valid.dns.",
		},
		valid: false,
	}, {
		name: "description too long",
		netboxIPSpec: NetBoxIPSpec{
			Address:     IP(net.IPv4(8, 8, 8, 8)),
			DNSName:     "valid.dns",
			Description: strings.Repeat("12345", 50),
		},
		valid: false,
	}, {
		name: "invalid tag slug",
		netboxIPSpec: NetBoxIPSpec{
			Address: IP(net.IPv4(8, 8, 8, 8)),
			DNSName: "valid.dns",
			Tags: []Tag{{
				Name: "good",
				Slug: "~bad~",
			}},
		},
		valid: false,
	}, {
		name: "valid with tags",
		netboxIPSpec: NetBoxIPSpec{
			Address: IP(net.IPv4(8, 8, 8, 8)),
			DNSName: "valid.dns",
			Tags: []Tag{{
				Name: "good",
				Slug: "also-good",
			}},
		},
		valid: true,
	}, {
		name: "valid with single-domain dns",
		netboxIPSpec: NetBoxIPSpec{
			Address: IP(net.IPv4(8, 8, 8, 8)),
			DNSName: "foo",
		},
		valid: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ip := &NetBoxIP{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       test.netboxIPSpec,
			}

			err := apiservervalidation.ValidateCustomResource(nil, ip, validator)
			t.Log(err)
			if err != nil && test.valid {
				t.Errorf("want no error, got %q\n", err)
			} else if err == nil && !test.valid {
				t.Error("want error, nil")
			}
		})
	}
}
