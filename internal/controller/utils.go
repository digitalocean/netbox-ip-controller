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
	"sort"
	"strings"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	netboxcrd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// IPs is a struct used to store the NetBoxIPs belonging to a pod or service.
// A nil value means the pod or service does not have an IP of that scheme.
// If dual stack is not enabled, at least one of the two IPs will be nil
type IPs struct {
	IPv4 *v1beta1.NetBoxIP
	IPv6 *v1beta1.NetBoxIP
}

// NetBoxIPConfig is a struct used to pass configuration parameters for
// the NetBoxIPs created by CreateNetBoxIPs
type NetBoxIPConfig struct {
	Object           client.Object
	DNSName          string
	ReconcilerTags   []netbox.Tag
	ReconcilerLabels map[string]bool
}

// CreateNetBoxIPs takes a slice of IP addresses in string form and creates
// NetBoxIPs according to the configuration specified by config
// The IP addresses are returned in the form of an IPs struct. If IPv4 or IPv6
// is nil in the returned struct, that means no IP address of that scheme was
// given as input
func CreateNetBoxIPs(ips []string, config NetBoxIPConfig) (*IPs, error) {

	labels := make([]string, 0)
	for key, value := range config.Object.GetLabels() {
		if config.ReconcilerLabels[key] {
			labels = append(labels, fmt.Sprintf("%s: %s", key, value))
		}
	}
	sort.Strings(labels)
	labels = append([]string{fmt.Sprintf("namespace: %s", config.Object.GetNamespace())}, labels...)

	var tags []v1beta1.Tag
	for _, tag := range config.ReconcilerTags {
		tags = append(tags, v1beta1.Tag{
			Name: tag.Name,
			Slug: tag.Slug,
		})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })

	var outputIPs IPs

	for _, ip := range ips {
		var addr netip.Addr
		if ip != "" && ip != "None" {
			var err error
			addr, err = netip.ParseAddr(ip)
			if err != nil {
				return &IPs{}, fmt.Errorf("invalid IP address: %w", err)
			}
		} else {
			continue
		}

		ipName := NetBoxIPName(config.Object, Scheme(addr))

		netBoxIP := &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       netboxcrd.NetBoxIPKind,
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ipName,
				Namespace: config.Object.GetNamespace(),
				Labels: map[string]string{
					netboxctrl.NameLabel: config.Object.GetName(),
				},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address:     addr,
				DNSName:     config.DNSName,
				Tags:        tags,
				Description: strings.Join(labels, ", "),
			},
		}

		if addr.Is4() {
			outputIPs.IPv4 = netBoxIP
		} else {
			outputIPs.IPv6 = netBoxIP
		}

	}

	return &outputIPs, nil
}

// NetBoxIPName derives NetBoxIP name from the object's metadata.
// suffix may be an empty string, in which case it is ignored.
// Otherwise, it is appended to the returned name to provide
// additional context to the IP (such as including the IP address'
// scheme as part of the name)
func NetBoxIPName(obj client.Object, suffix string) string {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	// use UIDs instead of names in case of name conflicts
	name := fmt.Sprintf("%s-%s", strings.ToLower(kind), obj.GetUID())
	if suffix != "" {
		name = fmt.Sprintf("%s-%s", name, suffix)
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
		existingIP.Finalizers = ip.Finalizers
		existingIP.Labels = ip.Labels
		if err := kubeClient.Update(ctx, &existingIP); err != nil {
			return fmt.Errorf("updating netboxip: %w", err)
		}
		ll.Info("updated netboxip")

		return nil
	})
}
