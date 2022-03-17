package pod

import (
	"context"
	"fmt"
	"net"
	"strings"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
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

// New returns a new Controller for pods.
func New(netboxClient netbox.Client, opts ...ctrl.Option) (ctrl.Controller, error) {
	s := &ctrl.Settings{
		NetboxClient: netboxClient,
	}
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, err
		}
	}

	return &controller{
		reconciler: &reconciler{
			netboxClient: netboxClient,
			tags:         s.Tags,
			labels:       s.Labels,
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
		Named("pod").
		For(&corev1.Pod{}).
		WithEventFilter(filter).
		Complete(c.reconciler)
}

type reconciler struct {
	netboxClient netbox.Client
	// client can be used to retrieve objects from the kubernetes APIServer
	client client.Client
	tags   []netbox.Tag
	labels map[string]bool
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
			return reconcile.Result{}, fmt.Errorf("retrieving pod: %w", err)
		}
		return reconcile.Result{}, nil
	}

	ll = ll.With(
		log.String("uid", string(pod.UID)),
		log.String("ip", pod.Status.PodIP),
	)

	ipKey := netbox.IPAddressKey{
		DNSName: pod.Name,
		UID:     string(pod.UID),
	}

	if !pod.DeletionTimestamp.IsZero() {
		// if deletion timestamp is set, that means the object is under deletion
		// and waiting for finalizers to be executed
		if err := r.netboxClient.DeleteIP(ctx, ipKey); err != nil {
			return reconcile.Result{}, fmt.Errorf("deleting IP: %w", err)
		}
		ll.Info("deleted IP: pod was removed")

		controllerutil.RemoveFinalizer(&pod, netboxctrl.IPFinalizer)
		err = r.client.Update(ctx, &pod)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}
	}

	if pod.Status.PodIP == "" {
		ip, err := r.netboxClient.GetIP(ctx, ipKey)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("checking if IP exists: %w", err)
		}
		if ip != nil {
			if err := r.netboxClient.DeleteIP(ctx, ipKey); err != nil {
				return reconcile.Result{}, fmt.Errorf("deleting IP: %w", err)
			}
			ll.Info("deleted IP: pod IP was removed")
		}

		return reconcile.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&pod, netboxctrl.IPFinalizer) {
		controllerutil.AddFinalizer(&pod, netboxctrl.IPFinalizer)
		err := r.client.Update(ctx, &pod)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("setting finalizer: %w", err)
		}
	}

	var labels []string
	for key, value := range pod.Labels {
		if r.labels[key] {
			labels = append(labels, fmt.Sprintf("%s: %s", key, value))
		}
	}

	_, err = r.netboxClient.UpsertIP(ctx, &netbox.IPAddress{
		UID:         string(pod.UID),
		DNSName:     pod.Name,
		Address:     net.ParseIP(pod.Status.PodIP),
		Tags:        r.tags,
		Description: strings.Join(labels, ", "),
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("upserting IP: %w", err)
	}
	ll.Info("upserted IP")

	return reconcile.Result{}, nil
}
