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

package v1beta1

import (
	"net/netip"
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidationSchema(t *testing.T) {
	var schema apiextensions.CustomResourceValidation
	if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(NetBoxIPValidationSchema, &schema, nil); err != nil {
		t.Errorf("converting CRD validation: %q\n", err)
	}
	validator, _, err := apiservervalidation.NewSchemaValidator(schema.OpenAPIV3Schema)
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
			Address: netip.AddrFrom4([4]byte{8, 8, 8, 8}),
			DNSName: "!?not.valid.dns.",
		},
		valid: false,
	}, {
		name: "description too long",
		netboxIPSpec: NetBoxIPSpec{
			Address:     netip.AddrFrom4([4]byte{8, 8, 8, 8}),
			DNSName:     "valid.dns",
			Description: strings.Repeat("12345", 50),
		},
		valid: false,
	}, {
		name: "invalid tag slug",
		netboxIPSpec: NetBoxIPSpec{
			Address: netip.AddrFrom4([4]byte{8, 8, 8, 8}),
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
			Address: netip.AddrFrom4([4]byte{8, 8, 8, 8}),
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
			Address: netip.AddrFrom4([4]byte{8, 8, 8, 8}),
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
