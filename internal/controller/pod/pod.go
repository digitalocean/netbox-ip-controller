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

package pod

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type controller struct {
	reconciler *reconciler
}

// New returns a new Controller for pods.
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
			tags:        s.Tags,
			labels:      s.Labels,
			log:         logger.With(log.String("reconciler", "pod")),
			dualStackIP: s.DualStackIP,
		},
	}, nil
}

// AddToManager attaches the controller to the given manager.
func (c *controller) AddToManager(mgr manager.Manager) error {
	return builder.
		ControllerManagedBy(mgr).
		Named("pod").
		For(&corev1.Pod{}).
		WithEventFilter(ctrl.OnCreateAndUpdateFilter).
		Complete(c.reconciler)
}

type reconciler struct {
	kubeClient  client.Client
	tags        []netbox.Tag
	labels      map[string]bool
	log         *log.Logger
	dualStackIP bool
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	r.log.Debug("setting client")
	r.kubeClient = c
	return nil
}

// Reconcile is called on every event that the given reconciler is watching,
// it updates pod IPs according to the pod changes.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ll := r.log.With(
		log.String("namespace", req.Namespace),
		log.String("name", req.Name),
	)

	ll.Info("reconciling pod")

	var pod corev1.Pod
	err := r.kubeClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &pod)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve pod", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("retrieving pod: %w", err)
		}
		return reconcile.Result{}, nil
	}

	if pod.Spec.HostNetwork {
		// a pod on host network will have the same IP as the node
		return reconcile.Result{}, nil
	}

	// ips is a slice to support dual stack IP addresses. If r.dualStackIP is false, ips will
	// always be a slice with 1 element
	ips, err := r.netboxipFromPod(&pod, r.dualStackIP)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, ip := range ips {

		if err := ctrl.DeclareOwner(ip, &pod); err != nil {
			return reconcile.Result{}, fmt.Errorf("setting owner: %w", err)
		}

		if pod.Status.PodIP == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
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

	return reconcile.Result{}, nil
}

func (r *reconciler) netboxipFromPod(pod *corev1.Pod, dualStack bool) ([]*v1beta1.NetBoxIP, error) {
	labels := []string{fmt.Sprintf("namespace: %s", pod.Namespace)}
	for key, value := range pod.Labels {
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

	var podIPs []string
	if dualStack {
		for _, ip := range pod.Status.PodIPs {
			podIPs = append(podIPs, ip.IP)
		}
	} else {
		podIPs = []string{pod.Status.PodIP}
	}

	var ips []*v1beta1.NetBoxIP
	for _, podIP := range podIPs {
		var addr netip.Addr
		if podIP != "" {
			var err error
			addr, err = netip.ParseAddr(podIP)
			if err != nil {
				return nil, fmt.Errorf("invalid IP address: %w", err)
			}
		}

		ips = append(ips, &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       netboxcrd.NetBoxIPKind,
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ctrl.NetBoxIPName(pod),
				Namespace: pod.Namespace,
				Labels: map[string]string{
					netboxctrl.NameLabel: pod.Name,
				},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address:     addr,
				DNSName:     pod.Name,
				Tags:        tags,
				Description: strings.Join(labels, ", "),
			},
		})
	}

	return ips, nil
}
