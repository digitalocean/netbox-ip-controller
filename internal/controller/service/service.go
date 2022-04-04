package service

import (
	"context"
	"fmt"
	"net"
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

// New returns a new Controller for services.
func New(opts ...ctrl.Option) (ctrl.Controller, error) {
	var s ctrl.Settings
	for _, o := range opts {
		if err := o(&s); err != nil {
			return nil, err
		}
	}

	return &controller{
		reconciler: &reconciler{
			tags:          s.Tags,
			labels:        s.Labels,
			clusterDomain: s.ClusterDomain,
			log:           log.L().With(log.String("reconciler", "service")),
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

	ip := r.netboxipFromService(&svc)
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

	return reconcile.Result{}, ctrl.UpsertNetBoxIP(ctx, r.kubeClient, ll, ip)
}

func (r *reconciler) netboxipFromService(svc *corev1.Service) *v1beta1.NetBoxIP {
	var labels []string
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

	ip := &v1beta1.NetBoxIP{
		TypeMeta: metav1.TypeMeta{
			Kind:       netboxcrd.NetBoxIPKind,
			APIVersion: "v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctrl.NetBoxIPName(svc),
			Namespace: svc.Namespace,
			Labels: map[string]string{
				netboxctrl.NameLabel: svc.Name,
			},
		},
		Spec: v1beta1.NetBoxIPSpec{
			Address:     v1beta1.IP(net.ParseIP(svc.Spec.ClusterIP)),
			DNSName:     fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, r.clusterDomain),
			Tags:        tags,
			Description: strings.Join(labels, ", "),
		},
	}

	return ip
}
