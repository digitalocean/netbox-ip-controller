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

package netboxip

import (
	"context"
	"errors"
	"fmt"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type controller struct {
	reconciler *reconciler
}

// New returns a new Controller for NetBoxIP resource.
func New(opts ...ctrl.Option) (ctrl.Controller, error) {
	var s ctrl.Settings
	for _, o := range opts {
		if err := o(&s); err != nil {
			return nil, err
		}
	}

	if s.NetBoxClient == nil {
		return nil, errors.New("netbox client is required for netboxip controller")
	}
	if err := s.NetBoxClient.UpsertUIDField(context.Background()); err != nil {
		return nil, fmt.Errorf("upserting UID field: %w", err)
	}

	logger := log.L()
	if s.Logger != nil {
		logger = s.Logger
	}

	return &controller{
		reconciler: &reconciler{
			netboxClient: s.NetBoxClient,
			log:          logger.With(log.String("reconciler", "netboxip")),
		},
	}, nil
}

// AddToManager attaches the controller to the given manager.
func (c *controller) AddToManager(mgr manager.Manager) error {
	return builder.
		ControllerManagedBy(mgr).
		Named("netboxip").
		For(&v1beta1.NetBoxIP{}).
		WithEventFilter(ctrl.OnCreateAndUpdateFilter).
		// with > 1 concurrent reconciles, we'd be risking creating
		// duplicate IPs in NetBox
		WithOptions(runtimecontroller.Options{MaxConcurrentReconciles: 1}).
		Complete(c.reconciler)
}

type reconciler struct {
	netboxClient netbox.Client
	kubeClient   client.Client
	log          *log.Logger
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

	ll.Info("reconciling netboxip")

	var ip v1beta1.NetBoxIP
	err := r.kubeClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &ip)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve netboxip", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("retrieving netboxip: %w", err)
		}
		return reconcile.Result{}, nil
	}

	ll = ll.With(
		log.String("uid", string(ip.UID)),
		log.Any("ip", ip.Spec.Address),
	)

	if !ip.DeletionTimestamp.IsZero() {
		// if deletion timestamp is set, that means the object is under deletion
		// and waiting for finalizers to be executed
		if err := r.netboxClient.DeleteIP(ctx, netbox.UID(ip.UID)); err != nil {
			return reconcile.Result{}, fmt.Errorf("deleting IP: %w", err)
		}
		ll.Info("deleted IP: netboxip was removed")

		controllerutil.RemoveFinalizer(&ip, netboxctrl.IPFinalizer)
		if err := r.kubeClient.Update(ctx, &ip); err != nil {
			return reconcile.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}

		return reconcile.Result{}, nil
	}

	// add finalizer to each fresh NetBoxIP
	if !controllerutil.ContainsFinalizer(&ip, netboxctrl.IPFinalizer) {
		controllerutil.AddFinalizer(&ip, netboxctrl.IPFinalizer)
		err := r.kubeClient.Update(ctx, &ip)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("setting finalizer: %w", err)
		}
	}

	var tags []netbox.Tag
	for _, t := range ip.Spec.Tags {
		tags = append(tags, netbox.Tag{
			Name: t.Name,
			Slug: t.Slug,
		})
	}

	_, err = r.netboxClient.UpsertIP(ctx, &netbox.IPAddress{
		UID:         netbox.UID(ip.UID),
		DNSName:     ip.Spec.DNSName,
		Address:     netbox.IP(ip.Spec.Address),
		Tags:        tags,
		Description: ip.Spec.Description,
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("upserting IP: %w", err)
	}
	ll.Info("upserted IP")

	return reconcile.Result{}, nil
}
