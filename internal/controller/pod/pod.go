package pod

import (
	"context"
	"fmt"
	"net"
	"strings"

	netboxcrd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"
	"github.com/pkg/errors"

	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
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
		// deletes cascade to all owned objects and don't need
		// to be handled explicitly
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
	kubeClient   client.Client
	tags         []netbox.Tag
	labels       map[string]bool
}

// InjectClient injects the client and implements inject.Client.
// A client will be automatically injected.
func (r *reconciler) InjectClient(c client.Client) error {
	log.L().Debug("setting client", log.String("reconciler", "pod"))
	r.kubeClient = c
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

	ipName := netboxipName(pod.Name)
	ip := r.netboxipFromPod(&pod)
	if err := declareOwner(ip, &pod); err != nil {
		return reconcile.Result{}, fmt.Errorf("setting owner: %w", err)
	}

	if pod.Status.PodIP == "" {
		if err := r.kubeClient.Delete(ctx, ip); client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("deleting netboxip: %w", err)
		}

		return reconcile.Result{}, nil
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existingIP v1beta1.NetBoxIP
		err := r.kubeClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: ipName}, &existingIP)
		if kubeerrors.IsNotFound(err) {
			if err := r.kubeClient.Create(ctx, ip); err != nil {
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
		if err := r.kubeClient.Update(ctx, &existingIP); err != nil {
			return fmt.Errorf("updating netboxip: %w", err)
		}
		ll.Info("updated netboxip")

		return nil
	})

	return reconcile.Result{}, err
}

func netboxipName(podName string) string {
	// pod names have the same length limit of 253 characters as NetBoxIPs,
	// so we cannot add any suffixes/prefixes
	return podName
}

func (r *reconciler) netboxipFromPod(pod *corev1.Pod) *v1beta1.NetBoxIP {
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

	ip := &v1beta1.NetBoxIP{
		TypeMeta: metav1.TypeMeta{
			Kind:       netboxcrd.NetBoxIPKind,
			APIVersion: "v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      netboxipName(pod.Name),
			Namespace: pod.Namespace,
		},
		Spec: v1beta1.NetBoxIPSpec{
			Address:     v1beta1.IP(net.ParseIP(pod.Status.PodIP)),
			DNSName:     pod.Name,
			Tags:        tags,
			Description: strings.Join(labels, ", "),
		},
	}

	return ip
}

func declareOwner(ip *v1beta1.NetBoxIP, pod *corev1.Pod) error {
	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("creating owner scheme: %w", err)
	}

	if err := controllerutil.SetControllerReference(pod, ip, scheme); err != nil {
		return errors.Wrap(err, "could not set pod as owner")
	}
	return nil
}
