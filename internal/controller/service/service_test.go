package service

import (
	"context"
	"fmt"
	"net"
	"testing"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	log "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcile(t *testing.T) {
	name := "foo"
	namespace := "test"
	serviceUID := "abc123"
	scheme := runtime.NewScheme()
	kubescheme.AddToScheme(scheme)
	v1beta1.AddToScheme(scheme)

	tests := []struct {
		name             string
		existingService  *corev1.Service
		existingNetBoxIP *v1beta1.NetBoxIP
		expectedNetBoxIP *v1beta1.NetBoxIP
	}{{
		name:             "does not exist",
		existingService:  nil,
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "with ClusterIP",
		existingService: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(serviceUID),
				Labels:    map[string]string{"app": "foo"},
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 8080}},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "192.168.0.1",
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("service-%s", serviceUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               name,
					UID:                types.UID(serviceUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: v1beta1.IP(net.IPv4(192, 168, 0, 1)),
				DNSName: fmt.Sprintf("%s.%s.svc.testclusterdomain", name, namespace),
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: "app: foo",
			},
		},
	}, {
		name: "without ClusterIP",
		existingService: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(serviceUID),
				Labels:    map[string]string{"app": "foo"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 8080}},
				Type:  corev1.ServiceTypeExternalName,
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "headless",
		existingService: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(serviceUID),
				Labels:    map[string]string{"app": "foo"},
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 8080}},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "None",
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "updated from with ClusterIP to without ClusterIP",
		existingService: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(serviceUID),
				Labels:    map[string]string{"app": "foo"},
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 8080}},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "",
			},
		},
		existingNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("service-%s", serviceUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               name,
					UID:                types.UID(serviceUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: v1beta1.IP(net.IPv4(192, 168, 0, 1)),
				DNSName: fmt.Sprintf("%s.%s.svc.testclusterdomain", name, namespace),
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: "app: foo",
			},
		},
		expectedNetBoxIP: nil,
	}, {
		name: "fix netboxip that got out of sync",
		existingService: &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(serviceUID),
				Labels:    map[string]string{"app": "foo"},
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 8080}},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "192.168.0.1",
			},
		},
		existingNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("service-%s", serviceUID),
				Namespace: namespace,
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: v1beta1.IP(net.IPv4(10, 0, 0, 1)),
			},
		},
		expectedNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("service-%s", serviceUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               name,
					UID:                types.UID(serviceUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: v1beta1.IP(net.IPv4(192, 168, 0, 1)),
				DNSName: fmt.Sprintf("%s.%s.svc.testclusterdomain", name, namespace),
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: "app: foo",
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kubeClientBuilder := fakeclient.NewClientBuilder().WithScheme(scheme)

			var existingObjs []client.Object
			if test.existingService != nil {
				existingObjs = append(existingObjs, test.existingService)
			}
			if test.existingNetBoxIP != nil {
				existingObjs = append(existingObjs, test.existingNetBoxIP)
			}
			kubeClientBuilder = kubeClientBuilder.WithObjects(existingObjs...)

			r := &reconciler{
				kubeClient:    kubeClientBuilder.Build(),
				clusterDomain: "testclusterdomain",
				tags:          []netbox.Tag{{Name: "bar", Slug: "bar"}},
				labels:        map[string]bool{"app": true},
				log:           log.L(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				},
			}

			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Errorf("reconciling: %q\n", err)
			}

			var actualNetBoxIP v1beta1.NetBoxIP
			err := r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("service-%s", serviceUID)}, &actualNetBoxIP)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP: %q\n", err)
			}

			if test.expectedNetBoxIP == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP not to exist, got %v\n", actualNetBoxIP)
			} else if test.expectedNetBoxIP != nil {
				if diff := cmp.Diff(test.expectedNetBoxIP, &actualNetBoxIP, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}
		})
	}
}
