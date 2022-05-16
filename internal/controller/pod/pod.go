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
			tags:   s.Tags,
			labels: s.Labels,
			log:    logger.With(log.String("reconciler", "pod")),
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
	kubeClient client.Client
	tags       []netbox.Tag
	labels     map[string]bool
	log        *log.Logger
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

	ip, err := r.netboxipFromPod(&pod)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := ctrl.DeclareOwner(ip, &pod); err != nil {
		return reconcile.Result{}, fmt.Errorf("setting owner: %w", err)
	}

	if pod.Status.PodIP == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		if err := r.kubeClient.Delete(ctx, ip); client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, fmt.Errorf("deleting netboxip: %w", err)
		}

		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, ctrl.UpsertNetBoxIP(ctx, r.kubeClient, ll, ip)
}

func (r *reconciler) netboxipFromPod(pod *corev1.Pod) (*v1beta1.NetBoxIP, error) {
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

	var addr netip.Addr
	if pod.Status.PodIP != "" {
		var err error
		addr, err = netip.ParseAddr(pod.Status.PodIP)
		if err != nil {
			return nil, fmt.Errorf("invalid IP address: %w", err)
		}
	}

	ip := &v1beta1.NetBoxIP{
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
	}

	return ip, nil
}
