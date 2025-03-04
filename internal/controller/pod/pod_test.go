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
	"net/netip"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func addrComparer(x netip.Addr, y netip.Addr) bool {
	return x.Compare(y) == 0
}

const (
	name      = "foo"
	namespace = "test"
	podUID    = "abc123"
)

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	kubescheme.AddToScheme(scheme)
	v1beta1.AddToScheme(scheme)

	tests := []struct {
		name             string
		existingPod      *corev1.Pod
		existingNetBoxIP *v1beta1.NetBoxIP
		expectedNetBoxIP *v1beta1.NetBoxIP
	}{{
		name:             "does not exist",
		existingPod:      nil,
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "with PodIP",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP: "192.168.0.1",
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}, {
		name: "without PodIP",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "updated from with PodIP to without PodIP",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
		},
		existingNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedNetBoxIP: nil,
	}, {
		name: "without publish labels",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"wrong_label": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP: "192.168.0.1",
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: nil,
	}, {
		name: "fix NetBoxIP that got out of sync",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP: "192.168.0.1",
			},
		},
		existingNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{10, 0, 0, 1}),
			},
		},
		expectedNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}, {
		name: "pod with existing IP enters completed phase",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP: "192.168.0.1",
				Phase: corev1.PodSucceeded,
			},
		},
		existingNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedNetBoxIP: nil,
	}, {
		// It is acceptable to have dual stack services/pods exist in a cluster even if
		// dual-stack-ips is disabled. In this case, only the PodIP should be registered into netbox
		name: "with dual stack PodIPs",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "192.168.0.1",
				PodIPs: []corev1.PodIP{{IP: "192.168.0.1"}, {IP: "1:2::3"}},
			},
		},
		existingNetBoxIP: nil,
		expectedNetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kubeClientBuilder := fakeclient.NewClientBuilder().WithScheme(scheme)

			var existingObjs []client.Object
			if test.existingPod != nil {
				existingObjs = append(existingObjs, test.existingPod)
			}
			if test.existingNetBoxIP != nil {
				existingObjs = append(existingObjs, test.existingNetBoxIP)
			}
			kubeClientBuilder = kubeClientBuilder.WithObjects(existingObjs...)

			r := &reconciler{
				kubeClient: kubeClientBuilder.Build(),
				tags:       []netbox.Tag{{Name: "bar", Slug: "bar"}},
				labels:     map[string]bool{"pod": true},
				log:        log.L(),
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
			err := r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("pod-%s-ipv4", podUID)}, &actualNetBoxIP)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP: %q\n", err)
			}

			if test.expectedNetBoxIP != nil && kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP to exist, but got not found error")
			} else if test.expectedNetBoxIP == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP not to exist, got %v\n", actualNetBoxIP)
			} else if test.expectedNetBoxIP != nil {
				if diff := cmp.Diff(test.expectedNetBoxIP, &actualNetBoxIP, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"), cmp.Comparer(addrComparer)); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}
		})
	}
}

func TestReconcileDualStack(t *testing.T) {

	scheme := runtime.NewScheme()
	kubescheme.AddToScheme(scheme)
	v1beta1.AddToScheme(scheme)

	tests := []struct {
		name                 string
		existingPod          *corev1.Pod
		existingNetBoxIPs    []*v1beta1.NetBoxIP
		expectedIPv4NetBoxIP *v1beta1.NetBoxIP
		expectedIPv6NetBoxIP *v1beta1.NetBoxIP
	}{{
		name:                 "does not exist",
		existingPod:          nil,
		existingNetBoxIPs:    nil,
		expectedIPv4NetBoxIP: nil,
		expectedIPv6NetBoxIP: nil,
	}, {
		name: "with ipv4 PodIP only",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "192.168.0.1",
				PodIPs: []corev1.PodIP{{IP: "192.168.0.1"}},
			},
		},
		existingNetBoxIPs: nil,
		expectedIPv4NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedIPv6NetBoxIP: nil,
	}, {
		name: "with ipv6 PodIP only",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "1:2::3",
				PodIPs: []corev1.PodIP{corev1.PodIP{IP: "1:2::3"}},
			},
		},
		existingNetBoxIPs:    nil,
		expectedIPv4NetBoxIP: nil,
		expectedIPv6NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}, {
		name: "with both IPv4 and IPv6 PodIPs",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "1:2::3",
				PodIPs: []corev1.PodIP{corev1.PodIP{IP: "1:2::3"}, corev1.PodIP{IP: "192.168.0.1"}},
			},
		},
		existingNetBoxIPs: nil,
		expectedIPv4NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedIPv6NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}, {
		name: "removed IPv6 address",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "192.168.0.1",
				PodIPs: []corev1.PodIP{corev1.PodIP{IP: "192.168.0.1"}},
			},
		},
		existingNetBoxIPs: []*v1beta1.NetBoxIP{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
		},
		expectedIPv4NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedIPv6NetBoxIP: nil,
	}, {
		name: "removed IPv4 address",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "1:2::3",
				PodIPs: []corev1.PodIP{corev1.PodIP{IP: "1:2::3"}},
			},
		},
		existingNetBoxIPs: []*v1beta1.NetBoxIP{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
		},
		expectedIPv4NetBoxIP: nil,
		expectedIPv6NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}, {
		name: "fix NetBoxIPs that got out of sync",
		existingPod: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(podUID),
				Labels:    map[string]string{"pod": "foo"},
			},
			Status: corev1.PodStatus{
				PodIP:  "1:2::4",
				PodIPs: []corev1.PodIP{corev1.PodIP{IP: "1:2::4"}, corev1.PodIP{IP: "192.168.0.2"}},
			},
		},
		existingNetBoxIPs: []*v1beta1.NetBoxIP{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetBoxIP",
					APIVersion: v1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
					Namespace: namespace,
					Labels:    map[string]string{netboxctrl.NameLabel: name},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               name,
						UID:                types.UID(podUID),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					}},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3}),
					DNSName: name,
					Tags: []v1beta1.Tag{{
						Name: "bar",
						Slug: "bar",
					}},
					Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
				},
			},
		},
		expectedIPv4NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv4", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 2}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
		expectedIPv6NetBoxIP: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%s-ipv6", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         ptr.To[bool](true),
					BlockOwnerDeletion: ptr.To[bool](true),
				}},
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom16([16]byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "bar",
				}},
				Description: fmt.Sprintf("namespace: %s, pod: foo", namespace),
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kubeClientBuilder := fakeclient.NewClientBuilder().WithScheme(scheme)

			var existingObjs []client.Object
			if test.existingPod != nil {
				existingObjs = append(existingObjs, test.existingPod)
			}
			if test.existingNetBoxIPs != nil {
				for _, existingIP := range test.existingNetBoxIPs {
					existingObjs = append(existingObjs, existingIP)
				}
			}
			kubeClientBuilder = kubeClientBuilder.WithObjects(existingObjs...)

			r := &reconciler{
				kubeClient:  kubeClientBuilder.Build(),
				tags:        []netbox.Tag{{Name: "bar", Slug: "bar"}},
				labels:      map[string]bool{"pod": true},
				log:         log.L(),
				dualStackIP: true,
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

			// Check for IPv4 NetBoxIP
			err := r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("pod-%s-ipv4", podUID)}, &actualNetBoxIP)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP: %q\n", err)
			}

			if test.expectedIPv4NetBoxIP != nil && kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP to exist, but got not found error")
			} else if test.expectedIPv4NetBoxIP == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want IPv4 NetBoxIP not to exist, got %v\n", actualNetBoxIP)
			} else if test.expectedIPv4NetBoxIP != nil {
				if diff := cmp.Diff(test.expectedIPv4NetBoxIP, &actualNetBoxIP, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"), cmp.Comparer(addrComparer)); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}

			// Check for IPv6 NetBoxIP
			err = r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("pod-%s-ipv6", podUID)}, &actualNetBoxIP)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP: %q\n", err)
			}

			if test.expectedIPv6NetBoxIP != nil && kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP to exist, but got not found error")
			} else if test.expectedIPv6NetBoxIP == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want IPv6 NetBoxIP not to exist, got %v\n", actualNetBoxIP)
			} else if test.expectedIPv6NetBoxIP != nil {
				if diff := cmp.Diff(test.expectedIPv6NetBoxIP, &actualNetBoxIP, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"), cmp.Comparer(addrComparer)); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}
		})
	}
}
