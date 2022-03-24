package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// SchemeGroupVersion is the group version used to register netbox objects.
	SchemeGroupVersion = schema.GroupVersion{Group: "netbox.digitalocean.com", Version: "v1beta1"}

	builder       = &scheme.Builder{GroupVersion: SchemeGroupVersion}
	schemeBuilder = builder.Register(&NetBoxIP{}, &NetBoxIPList{})

	// AddToScheme is the default scheme applier.
	AddToScheme = schemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group-qualified GroupResource.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}
