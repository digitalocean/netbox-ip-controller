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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcile(t *testing.T) {
	name := "foo"
	namespace := "test"
	podUID := "abc123"
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
				Name:      fmt.Sprintf("pod-%s", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
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
				Name:      fmt.Sprintf("pod-%s", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
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
				Name:      fmt.Sprintf("pod-%s", podUID),
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
				Name:      fmt.Sprintf("pod-%s", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
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
	}, {
		name: "Pod with existing IP enters completed phase",
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
				Name:      fmt.Sprintf("pod-%s", podUID),
				Namespace: namespace,
				Labels:    map[string]string{netboxctrl.NameLabel: name},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Pod",
					Name:               name,
					UID:                types.UID(podUID),
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
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
			err := r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("pod-%s", podUID)}, &actualNetBoxIP)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP: %q\n", err)
			}

			if test.expectedNetBoxIP != nil && kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP to exist, but got not found error")
			} else if test.expectedNetBoxIP == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP not to exist, got %v\n", actualNetBoxIP)
			} else if test.expectedNetBoxIP != nil {
				if diff := cmp.Diff(test.expectedNetBoxIP, &actualNetBoxIP, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"), cmpopts.IgnoreUnexported(netip.Addr{})); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}
		})
	}
}
