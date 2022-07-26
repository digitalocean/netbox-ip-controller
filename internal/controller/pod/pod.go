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

	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"
	"github.com/hashicorp/go-multierror"

	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
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

	ips, err := r.netboxIPsFromPod(&pod, r.dualStackIP)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create/update non-nil NetBoxIPs
	for _, ip := range []*v1beta1.NetBoxIP{ips.IPv4, ips.IPv6} {
		if ip == nil || !r.podShouldHaveIP(&pod) {
			continue
		}

		if err := ctrl.DeclareOwner(ip, &pod); err != nil {
			return reconcile.Result{}, fmt.Errorf("setting owner: %w", err)
		}

		if err = ctrl.UpsertNetBoxIP(ctx, r.kubeClient, ll, ip); err != nil {
			return reconcile.Result{}, err
		}
	}

	// For both IPv4 and IPv6 addresses, delete the associated NetBoxIP object (if it exists) if the Pod
	// no longer has an address of that scheme assigned, or if the pod has entered a succeeded or failed phase.
	// This is because if the pod has entered a completed phase, its IP may be re-used by another pod.

	var errs multierror.Error
	if err = r.deleteNetBoxIPIfStale(ctx, ips.IPv4, pod, "ipv4"); err != nil {
		multierror.Append(&errs, err)
	}

	if err = r.deleteNetBoxIPIfStale(ctx, ips.IPv6, pod, "ipv6"); err != nil {
		multierror.Append(&errs, err)
	}

	if errs.ErrorOrNil() != nil {
		return reconcile.Result{}, &errs
	}

	return reconcile.Result{}, nil
}

func (r *reconciler) netboxIPsFromPod(pod *corev1.Pod, dualStack bool) (*ctrl.IPs, error) {
	var podIPs []string
	if dualStack {
		for _, ip := range pod.Status.PodIPs {
			podIPs = append(podIPs, ip.IP)
		}
	} else {
		podIPs = []string{pod.Status.PodIP}
	}

	ips, err := ctrl.CreateNetBoxIPs(podIPs, ctrl.NetBoxIPConfig{
		Object:           pod,
		DNSName:          pod.Name,
		ReconcilerTags:   r.tags,
		ReconcilerLabels: r.labels,
	})
	if err != nil {
		return &ctrl.IPs{}, err
	}

	return ips, nil
}

func (r *reconciler) deleteNetBoxIPIfStale(ctx context.Context, netboxip *v1beta1.NetBoxIP, pod corev1.Pod, suffix string) error {
	var ip v1beta1.NetBoxIP
	err := r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: pod.Namespace, Name: ctrl.NetBoxIPName(&pod, suffix)}, &ip)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("fetching NetBoxIP: %q", err)
	} else if !kubeerrors.IsNotFound(err) {
		if netboxip == nil || !r.podShouldHaveIP(&pod) {
			if err := r.kubeClient.Delete(ctx, &ip); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("deleting netboxip: %w", err)
			}
		}
	}
	return nil
}

func (r *reconciler) podShouldHaveIP(pod *corev1.Pod) bool {
	return ctrl.HasPublishLabels(r.labels, pod.Labels) &&
		!(pod.Status.PodIP == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed)
}
