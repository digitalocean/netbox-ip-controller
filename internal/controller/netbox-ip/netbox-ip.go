package pod

import (
	"context"
	"fmt"

	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		reconciler: &reconciler{},
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
		Named("netbox-ip").
		For(&v1beta1.NetBoxIP{}).
		WithEventFilter(filter).
		Complete(c.reconciler)
}

type reconciler struct {
	// client can be used to retrieve objects from the kubernetes APIServer
	client client.Client
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	log.L().Debug("setting client", log.String("reconciler", "netbox-ip"))
	r.client = c
	return nil
}

// Reconcile is called on every event that the given reconciler is watching,
// it updates pod IPs according to the pod changes.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ll := log.L().With(
		log.String("reconciler", "netbox-ip"),
		log.String("namespace", req.Namespace),
		log.String("name", req.Name),
	)

	ll.Info("reconciling NetBoxIP")

	var ip v1beta1.NetBoxIP
	err := r.client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &ip)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve NetBoxIP", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("retrieving NetBoxIP: %w", err)
		}
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}
