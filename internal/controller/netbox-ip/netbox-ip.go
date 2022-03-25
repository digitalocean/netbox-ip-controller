package netboxip

import (
	"context"
	"fmt"
	"net"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type controller struct {
	reconciler *reconciler
}

// New returns a new Controller for NetBoxIP resource.
func New(netboxClient netbox.Client) (ctrl.Controller, error) {
	return &controller{
		reconciler: &reconciler{
			netboxClient: netboxClient,
		},
	}, nil
}

var filter = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return e.Object != nil
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectNew != nil
	},
	DeleteFunc: func(_ event.DeleteEvent) bool {
		// we delete the IP when the pod gets a deletionTimestamp,
		// which falls under UpdateFunc
		return false
	},
}

// AddToManager attaches the controller to the given manager.
func (c *controller) AddToManager(mgr manager.Manager) error {
	return builder.
		ControllerManagedBy(mgr).
		Named("netboxip").
		For(&v1beta1.NetBoxIP{}).
		WithEventFilter(filter).
		Complete(c.reconciler)
}

type reconciler struct {
	netboxClient netbox.Client
	kubeClient   client.Client
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	log.L().Debug("setting client", log.String("reconciler", "netbox-ip"))
	r.kubeClient = c
	return nil
}

// Reconcile is called on every event that the given reconciler is watching,
// it updates pod IPs according to the pod changes.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ll := log.L().With(
		log.String("reconciler", "netboxip"),
		log.String("namespace", req.Namespace),
		log.String("name", req.Name),
	)

	ll.Info("reconciling NetBoxIP")

	var ip v1beta1.NetBoxIP
	err := r.kubeClient.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &ip)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve NetBoxIP", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("retrieving NetBoxIP: %w", err)
		}
		return reconcile.Result{}, nil
	}

	ll = ll.With(
		log.String("uid", string(ip.UID)),
		log.Any("ip", ip.Spec.Address),
	)

	ipKey := netbox.IPAddressKey{
		DNSName: ip.Spec.DNSName,
		UID:     string(ip.UID),
	}

	if !ip.DeletionTimestamp.IsZero() {
		// if deletion timestamp is set, that means the object is under deletion
		// and waiting for finalizers to be executed
		if err := r.netboxClient.DeleteIP(ctx, ipKey); err != nil {
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
		UID:         string(ip.UID),
		DNSName:     ip.Spec.DNSName,
		Address:     net.IP(ip.Spec.Address),
		Tags:        tags,
		Description: ip.Spec.Description,
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("upserting IP: %w", err)
	}
	ll.Info("upserted IP")

	return reconcile.Result{}, nil
}
