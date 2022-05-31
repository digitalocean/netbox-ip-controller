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

package controller

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"

	log "go.uber.org/zap"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// NetBoxIPName derives NetBoxIP name from the object's metadata.
// scheme may be an empty string, in which case it is ignored.
// Otherwise, it is appended to the returned name to distinguish
// IPs belonging to the same pod or service (to support dual stack IPs).
func NetBoxIPName(obj client.Object, scheme string) string {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	// use UIDs instead of names in case of name conflicts
	name := fmt.Sprintf("%s-%s", strings.ToLower(kind), obj.GetUID())
	if scheme != "" {
		name = fmt.Sprintf("%s-%s", name, scheme)
		fmt.Println(name)
	}
	return name
}

// Scheme returns the name of the scheme of the given IP (ipv4 or ipv6),
// or an empty string if ip is not a valid IP address.
func Scheme(ip netip.Addr) string {
	if ip.Is6() || ip.Is4In6() {
		return "ipv6"
	} else if ip.Is4() {
		return "ipv4"
	} else {
		return ""
	}
}

// DeclareOwner sets the provided object as the controller of
// the given NetBoxIP.
func DeclareOwner(ip *v1beta1.NetBoxIP, obj client.Object) error {
	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("creating owner scheme: %w", err)
	}

	if err := controllerutil.SetControllerReference(obj, ip, scheme); err != nil {
		return fmt.Errorf("could not set owner: %w", err)
	}
	return nil
}

// UpsertNetBoxIP creates or updates (if exists) the NetBoxIP provided.
func UpsertNetBoxIP(ctx context.Context, kubeClient client.Client, ll *log.Logger, ip *v1beta1.NetBoxIP) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existingIP v1beta1.NetBoxIP
		err := kubeClient.Get(ctx, client.ObjectKey{Namespace: ip.Namespace, Name: ip.Name}, &existingIP)
		if kubeerrors.IsNotFound(err) {
			if err := kubeClient.Create(ctx, ip); err != nil {
				return fmt.Errorf("creating netboxip: %w", err)
			}
			ll.Info("created netboxip")
			return nil
		} else if err != nil {
			return fmt.Errorf("retrieving netboxip: %w", err)
		}

		if !ip.Spec.Changed(existingIP.Spec) {
			return nil
		}

		existingIP.Spec = ip.Spec
		existingIP.OwnerReferences = ip.OwnerReferences
		existingIP.Labels = ip.Labels
		if err := kubeClient.Update(ctx, &existingIP); err != nil {
			return fmt.Errorf("updating netboxip: %w", err)
		}
		ll.Info("updated netboxip")

		return nil
	})
}
