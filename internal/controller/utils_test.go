package controller

import (
	"net/netip"
	"testing"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	netboxcrd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateNetBoxIPs(t *testing.T) {
	tests := []struct {
		name        string
		ips         []string
		config      NetBoxIPConfig
		expectedIPs *IPs
	}{{
		name: "labels should be sorted",
		ips:  []string{"192.168.0.1"},
		config: NetBoxIPConfig{
			Object: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testpod",
					Namespace: "testnamespace",
					UID:       types.UID("abc123"),
					Labels: map[string]string{
						"c":          "foo",
						"b":          "bar",
						"a":          "baz",
						"irrelevant": "",
					},
				},
			},
			ReconcilerLabels: map[string]bool{"a": true, "b": true, "c": true},
		},
		expectedIPs: &IPs{
			IPv4: &v1beta1.NetBoxIP{
				TypeMeta: metav1.TypeMeta{
					Kind:       netboxcrd.NetBoxIPKind,
					APIVersion: "v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-abc123-ipv4",
					Namespace: "testnamespace",
					Labels: map[string]string{
						netboxctrl.NameLabel: "testpod",
					},
					Finalizers: []string{netboxctrl.IPFinalizer},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address:     netip.AddrFrom4([4]byte{192, 168, 0, 1}),
					Description: "namespace: testnamespace, a: baz, b: bar, c: foo",
				},
			},
		},
	}, {
		name: "tags should be sorted",
		ips:  []string{"192.168.0.1"},
		config: NetBoxIPConfig{
			Object: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testpod",
					Namespace: "testnamespace",
					UID:       types.UID("abc123"),
				},
			},
			ReconcilerTags: []netbox.Tag{{
				Name: "ytest",
				Slug: "1slug",
			}, {
				Name: "xtest",
				Slug: "2slug",
			}, {
				Name: "ztest",
				Slug: "3slug",
			}},
			ReconcilerLabels: map[string]bool{"a": true, "b": true, "c": true},
		},
		expectedIPs: &IPs{
			IPv4: &v1beta1.NetBoxIP{
				TypeMeta: metav1.TypeMeta{
					Kind:       netboxcrd.NetBoxIPKind,
					APIVersion: "v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-abc123-ipv4",
					Namespace: "testnamespace",
					Labels: map[string]string{
						netboxctrl.NameLabel: "testpod",
					},
					Finalizers: []string{netboxctrl.IPFinalizer},
				},
				Spec: v1beta1.NetBoxIPSpec{
					Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
					Tags: []v1beta1.Tag{{
						Name: "xtest",
						Slug: "2slug",
					}, {
						Name: "ytest",
						Slug: "1slug",
					}, {
						Name: "ztest",
						Slug: "3slug",
					}},
					Description: "namespace: testnamespace",
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ips, err := CreateNetBoxIPs(test.ips, test.config)
			if err != nil {
				t.Errorf("expected no error, got %q", err)
			}

			if diff := cmp.Diff(
				test.expectedIPs,
				ips,
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmp.Comparer(func(x, y netip.Addr) bool { return x.Compare(y) == 0 }),
			); diff != "" {
				t.Errorf("IPs (-want, +got)\n%s", diff)
			}
		})
	}
}
