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

package service

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	netboxcrd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type controller struct {
	reconciler *reconciler
}

// New returns a new Controller for services.
func New(opts ...ctrl.Option) (ctrl.Controller, error) {
	var s ctrl.Settings
	for _, o := range opts {
		if err := o(&s); err != nil {
			return nil, err
		}
	}

	logger := log.L()
	if s.Logger != nil {
		logger = s.Logger
	}

	return &controller{
		reconciler: &reconciler{
			tags:          s.Tags,
			labels:        s.Labels,
			clusterDomain: s.ClusterDomain,
			log:           logger.With(log.String("reconciler", "service")),
			dualStackIP:   s.DualStackIP,
		},
	}, nil
}

// AddToManager attaches the controller to the given manager.
func (c *controller) AddToManager(mgr manager.Manager) error {
	return builder.
		ControllerManagedBy(mgr).
		Named("service").
		For(&corev1.Service{}).
		WithEventFilter(ctrl.OnCreateAndUpdateFilter).
		Complete(c.reconciler)
}

type reconciler struct {
	kubeClient    client.Client
	tags          []netbox.Tag
	labels        map[string]bool
	clusterDomain string
	log           *log.Logger
	dualStackIP   bool
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	r.log.Debug("setting client")
	r.kubeClient = c
	return nil
}

// Reconcile is called on every event that the given reconciler is watching,
// it updates service IPs according to the service changes.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ll := r.log.With(
		log.String("namespace", req.Namespace),
		log.String("name", req.Name),
	)

	ll.Info("reconciling service")

	var svc corev1.Service
	err := r.kubeClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &svc)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve service", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("retrieving service: %w", err)
		}
		return reconcile.Result{}, nil
	}

	// ips is a slice to support dual stack IP addresses. If r.dualStackIP is false, ips will
	// always be a slice with 1 element
	ips, err := r.netboxipFromService(&svc, r.dualStackIP)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, ip := range ips {

		if err := ctrl.DeclareOwner(ip, &svc); err != nil {
			return reconcile.Result{}, fmt.Errorf("setting owner: %w", err)
		}

		// in addition to ClusterIP type services, LoadBalancer and NodePort
		// also have an underlying ClusterIP; we're not publishing external IPs
		// for such services though
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
			// ClusterIP may be changed to "" when service type is changed to ExternalName;
			// "None" corresponds to a headless service
			if err := r.kubeClient.Delete(ctx, ip); client.IgnoreNotFound(err) != nil {
				return reconcile.Result{}, fmt.Errorf("deleting netboxip: %w", err)
			}

			return reconcile.Result{}, nil
		}

		err = ctrl.UpsertNetBoxIP(ctx, r.kubeClient, ll, ip)
		if err != nil {
			return reconcile.Result{}, err
		}

	}

	// If dual stack IPs are enabled, it's possible that one IP was deleted but another still
	// exists for the service. For both IPv4 and IPv6 NetBoxIPs (if any), check if their addresses
	// are still used by the service and delete if not.
	if r.dualStackIP {
		var v4IP v1beta1.NetBoxIP
		err = r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: svc.Namespace, Name: ctrl.NetBoxIPName(&svc, "ipv4")}, &v4IP)
		if client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("fetching NetBoxIP: %q", err)
		} else if !kubeerrors.IsNotFound(err) {
			if isStaleScheme(svc, v4IP) {
				if err := r.kubeClient.Delete(ctx, &v4IP); client.IgnoreNotFound(err) != nil {
					return reconcile.Result{}, fmt.Errorf("deleting netboxip: %w", err)
				}
			}
		}

		var v6IP v1beta1.NetBoxIP
		err = r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: svc.Namespace, Name: ctrl.NetBoxIPName(&svc, "ipv6")}, &v6IP)
		if client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("fetching NetBoxIP: %q", err)
		} else if !kubeerrors.IsNotFound(err) {
			if isStaleScheme(svc, v6IP) {
				if err := r.kubeClient.Delete(ctx, &v6IP); client.IgnoreNotFound(err) != nil {
					return reconcile.Result{}, fmt.Errorf("deleting netboxip: %w", err)
				}
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *reconciler) netboxipFromService(svc *corev1.Service, dualStack bool) ([]*v1beta1.NetBoxIP, error) {
	labels := []string{fmt.Sprintf("namespace: %s", svc.Namespace)}
	for key, value := range svc.Labels {
		if r.labels[key] {
			labels = append(labels, fmt.Sprintf("%s: %s", key, value))
		}
	}

	var tags []v1beta1.Tag
	for _, tag := range r.tags {
		tags = append(tags, v1beta1.Tag{
			Name: tag.Name,
			Slug: tag.Slug,
		})
	}

	var svcIPs []string
	if dualStack {
		svcIPs = svc.Spec.ClusterIPs
	} else {
		svcIPs = []string{svc.Spec.ClusterIP}
	}

	var ips []*v1beta1.NetBoxIP
	for _, svcIP := range svcIPs {
		var addr netip.Addr
		if svcIP != "" && svcIP != "None" {
			var err error
			addr, err = netip.ParseAddr(svcIP)
			if err != nil {
				return nil, fmt.Errorf("invalid IP address: %w", err)
			}
		}

		var scheme string
		if dualStack {
			scheme = ctrl.Scheme(addr)
		}

		ips = append(ips, &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       netboxcrd.NetBoxIPKind,
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctrl.NetBoxIPName(svc, scheme),
				Namespace: svc.Namespace,
				Labels: map[string]string{
					netboxctrl.NameLabel: svc.Name,
				},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address:     addr,
				DNSName:     fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, r.clusterDomain),
				Tags:        tags,
				Description: strings.Join(labels, ", "),
			},
		})

	}

	return ips, nil
}

// Returns true if the scheme of the given NetBoxIP is not currently
// used by the given service
func isStaleScheme(svc corev1.Service, ip v1beta1.NetBoxIP) bool {
	netboxIPScheme := ctrl.Scheme(ip.Spec.Address)
	for _, addr := range svc.Spec.ClusterIPs {
		var thisScheme string
		if strings.Contains(addr, ".") {
			thisScheme = "ipv4"
		} else if strings.Contains(addr, ":") {
			thisScheme = "ipv6"
		}
		if thisScheme == netboxIPScheme {
			return false
		}
	}
	return true
}
