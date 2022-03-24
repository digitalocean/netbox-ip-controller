package netbox

import (
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// GroupName is the unique namespace name for the resources.
	GroupName = "netbox.digitalocean.com"

	// NetBoxIPKind is the kind of the CRD.
	NetBoxIPKind = "NetBoxIP"

	// NetBoxIPPlural is the plural form of the CRD.
	NetBoxIPPlural = "netboxips"

	// NetBoxIPCRDName is the full name of the CRD.
	NetBoxIPCRDName = NetBoxIPPlural + "." + GroupName
)

var (
	// NetBoxIPShortNames is the list of short names for the CRD.
	NetBoxIPShortNames = []string{"netboxip"}

	// NetBoxIPCRD is the full custom resource definition.
	NetBoxIPCRD = &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: NetBoxIPCRDName,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: GroupName,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     NetBoxIPPlural,
				Kind:       NetBoxIPKind,
				ShortNames: NetBoxIPShortNames,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Schema:  v1beta1.NetBoxIPValidationSchema,
				AdditionalPrinterColumns: []apiextensionsv1.CustomResourceColumnDefinition{
					{
						Name:     "address",
						Type:     "string",
						JSONPath: ".spec.address",
					}, {
						Name:     "dnsname",
						Type:     "string",
						JSONPath: ".spec.dnsName",
					},
				},
			}},
		},
	}
)
