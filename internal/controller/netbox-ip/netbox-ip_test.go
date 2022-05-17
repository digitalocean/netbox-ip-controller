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

package netboxip

import (
	"context"
	"net/netip"
	"testing"
	"time"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	log "go.uber.org/zap"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcile(t *testing.T) {
	name := "foo"
	namespace := "test"
	uid := "123abc"
	scheme := runtime.NewScheme()
	v1beta1.AddToScheme(scheme)
	now := metav1.NewTime(time.Now())

	tests := []struct {
		name                string
		existingIPInNetBox  *netbox.IPAddress
		existingNetBoxIPObj *v1beta1.NetBoxIP
		expectedIPInNetBox  *netbox.IPAddress
		expectedNetBoxIPObj *v1beta1.NetBoxIP
	}{{
		name:                "netboxip does not exist",
		existingIPInNetBox:  nil,
		existingNetBoxIPObj: nil,
		expectedIPInNetBox:  nil,
		expectedNetBoxIPObj: &v1beta1.NetBoxIP{},
	}, {
		name:               "new netboxip created",
		existingIPInNetBox: nil,
		existingNetBoxIPObj: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				UID:       types.UID(uid),
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "baz",
				}},
			},
		},
		expectedIPInNetBox: &netbox.IPAddress{
			UID:     netbox.UID(uid),
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			DNSName: name,
			Tags: []netbox.Tag{{
				Name: "bar",
				Slug: "baz",
			}},
		},
		expectedNetBoxIPObj: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				UID:        types.UID(uid),
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "baz",
				}},
			},
		},
	}, {
		name: "existing netboxip updated",
		existingIPInNetBox: &netbox.IPAddress{
			UID:     netbox.UID(uid),
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			DNSName: name,
			Tags: []netbox.Tag{{
				Name: "fuz",
				Slug: "fur",
			}},
		},
		existingNetBoxIPObj: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				UID:        types.UID(uid),
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "baz",
				}},
			},
		},
		expectedIPInNetBox: &netbox.IPAddress{
			UID:     netbox.UID(uid),
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			DNSName: name,
			Tags: []netbox.Tag{{
				Name: "bar",
				Slug: "baz",
			}},
		},
		expectedNetBoxIPObj: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				UID:        types.UID(uid),
				Finalizers: []string{netboxctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "baz",
				}},
			},
		},
	}, {
		name: "netboxip deleted",
		existingIPInNetBox: &netbox.IPAddress{
			UID:     netbox.UID(uid),
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			DNSName: name,
			Tags: []netbox.Tag{{
				Name: "fuz",
				Slug: "fur",
			}},
		},
		existingNetBoxIPObj: &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: v1beta1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         namespace,
				UID:               types.UID(uid),
				Finalizers:        []string{netboxctrl.IPFinalizer},
				DeletionTimestamp: &now,
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: name,
				Tags: []v1beta1.Tag{{
					Name: "bar",
					Slug: "baz",
				}},
			},
		},
		expectedIPInNetBox:  nil,
		expectedNetBoxIPObj: &v1beta1.NetBoxIP{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existingIPs := make(map[netbox.UID]netbox.IPAddress)
			if test.existingIPInNetBox != nil {
				existingIPs[netbox.UID(uid)] = *test.existingIPInNetBox
			}

			kubeClientBuilder := fakeclient.NewClientBuilder().WithScheme(scheme)
			if test.existingNetBoxIPObj != nil {
				kubeClientBuilder = kubeClientBuilder.WithObjects(test.existingNetBoxIPObj)
			}

			r := &reconciler{
				netboxClient: netbox.NewFakeClient(nil, existingIPs),
				kubeClient:   kubeClientBuilder.Build(),
				log:          log.L(),
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test",
					Name:      "foo",
				},
			}

			if _, err := r.Reconcile(context.Background(), req); err != nil {
				t.Errorf("reconciling: %q\n", err)
			}

			actualIPInNetBox, err := r.netboxClient.GetIP(context.Background(), netbox.UID(uid))
			if err != nil {
				t.Errorf("fetching IP from NetBox: %q\n", err)
			}

			if diff := cmp.Diff(test.expectedIPInNetBox, actualIPInNetBox, cmpopts.IgnoreUnexported(netbox.IP{})); diff != "" {
				t.Errorf("IP in NetBox (-want, +got)\n%s", diff)
			}

			var actualNetBoxIPObj v1beta1.NetBoxIP
			err = r.kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, &actualNetBoxIPObj)
			if err != nil && !kubeerrors.IsNotFound(err) {
				t.Fatalf("fetching NetBoxIP object: %q\n", err)
			}

			if test.expectedNetBoxIPObj == nil && !kubeerrors.IsNotFound(err) {
				t.Errorf("want NetBoxIP not to exist, got %v\n", actualNetBoxIPObj)
			} else if test.expectedNetBoxIPObj != nil {
				if diff := cmp.Diff(test.expectedNetBoxIPObj, &actualNetBoxIPObj, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"), cmpopts.IgnoreUnexported(netip.Addr{})); diff != "" {
					t.Errorf("NetBoxIP object (-want, +got)\n%s", diff)
				}
			}
		})
	}
}
