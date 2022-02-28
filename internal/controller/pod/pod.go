package pod

import (
	"context"
	"fmt"

	netbox "github.com/netbox-community/go-netbox/netbox/client"
	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Controller is responsible for updating IPs of a single k8s resource.
type Controller struct {
	reconciler *reconciler
}

// New returns a new Controller.
func New(netboxClient *netbox.NetBoxAPI) *Controller {
	return &Controller{
		reconciler: &reconciler{
			netboxClient: netboxClient,
		},
	}
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
func (c *Controller) AddToManager(mgr manager.Manager) error {
	return builder.
		ControllerManagedBy(mgr).
		Named("pod").
		For(&corev1.Pod{}).
		WithEventFilter(filter).
		Complete(c.reconciler)
}

type reconciler struct {
	netboxClient *netbox.NetBoxAPI
	// client can be used to retrieve objects from the kubernetes APIServer
	client client.Client
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	log.L().Debug("setting client", log.String("reconciler", "pod"))
	r.client = c
	return nil
}

// Reconcile is called on every event that the given reconciler is watching,
// it updates pod IPs according to the pod changes.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ll := log.L().With(
		log.String("reconciler", "pod"),
		log.String("namespace", req.Namespace),
		log.String("name", req.Name),
	)

	ll.Info("reconciling pod IP")

	var pod corev1.Pod
	err := r.client.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &pod)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			ll.Error("failed to retrieve pod", log.Error(err))
			return reconcile.Result{}, fmt.Errorf("failed to retrieve pod: %s", err)
		}
		return reconcile.Result{}, nil
	}

	// TODO: actually do something

	return reconcile.Result{}, nil
}
