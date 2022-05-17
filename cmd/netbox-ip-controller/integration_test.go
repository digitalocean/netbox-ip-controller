//go:build sandbox
// +build sandbox

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

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"testing"
	"time"

	netboxipctrl "github.com/digitalocean/netbox-ip-controller"
	crd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	crdclient "github.com/digitalocean/netbox-ip-controller/client/clientset/versioned"
	"github.com/digitalocean/netbox-ip-controller/internal/crdregistration"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"
	"golang.org/x/time/rate"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	netboxAPIURL = "http://netbox:8080/api"
	netboxToken  = "48c7ba92-0f82-443a-8cf3-981559ff32cf"
	serviceCIDR  = "192.168.0.0/24"
	backoff1min  = wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   1,
		Steps:    20,
	}
	logger = zap.L()
)

func TestMain(m *testing.M) {
	var err error
	logger, err = zap.NewDevelopment()
	if err != nil {
		log.Fatal("failed to initialize logger", err)
	}
	defer logger.Sync()
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestController(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env, err := newTestEnvWithController(ctx, t)
	defer env.Stop()
	if err != nil {
		logger.Fatal("failed to start test env", zap.Error(err))
	}

	tests := map[string]func(t *testing.T, env *testEnv){
		"TestPod":      testPod,
		"TestService":  testService,
		"TestNetBoxIP": testNetBoxIP,
	}

	for name, testFunc := range tests {
		t.Run(name, func(t *testing.T) {
			testFunc(t, env)
		})
	}
}

func testPod(t *testing.T, env *testEnv) {
	namespace := "testpod"

	testFunc := func() {
		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: namespace,
				Labels: map[string]string{
					"app":              "foo",
					"irrelevant_label": "bar",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "redis",
					Image: "redis:6",
				}},
			},
		}

		var err error
		pod, err = env.KubeClient.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("creating pod: %q\n", err)
		}

		pod.Status = corev1.PodStatus{
			PodIP: "172.17.0.1",
		}
		pod, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating pod: %q\n", err)
		}

		netboxipName := fmt.Sprintf("pod-%s", pod.UID)
		var netboxip *v1beta1.NetBoxIP
		err = retry.OnError(
			backoff1min,
			func(err error) bool { return kubeerrors.IsNotFound(err) },
			func() error {
				netboxip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Get(context.Background(), netboxipName, metav1.GetOptions{})
				return err
			})
		if err != nil {
			t.Errorf("waiting for netboxip: %q", err)
		}

		expectedIP := &netbox.IPAddress{
			UID:     netbox.UID(netboxip.UID),
			DNSName: pod.Name,
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			Tags: []netbox.Tag{
				{Name: "kubernetes", Slug: "kubernetes"},
				{Name: "k8s-pod", Slug: "k8s-pod"},
			},
			Description: fmt.Sprintf("namespace: %s, app: foo", namespace),
		}

		if _, err := env.WaitForIP(expectedIP); err != nil {
			t.Fatal(err)
		}

		// delete IP from pod status and expect the IP to be removed from NetBox
		pod, err = env.KubeClient.CoreV1().Pods(namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("retrieving current pod: %q\n", err)
		}
		pod.Status = corev1.PodStatus{}
		_, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating pod: %q\n", err)
		}

		err = env.WaitForIPDeletion(netbox.UID(netboxip.UID))
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func testService(t *testing.T, env *testEnv) {
	namespace := "testservice"

	testFunc := func() {
		svc := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: namespace,
				Labels: map[string]string{
					"app":              "foo",
					"irrelevant_label": "bar",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Port: 8080}},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "192.168.0.5",
			},
		}

		var err error
		svc, err = env.KubeClient.CoreV1().Services(namespace).Create(context.Background(), svc, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("creating service: %q\n", err)
		}

		netboxipName := fmt.Sprintf("service-%s", svc.UID)
		var netboxip *v1beta1.NetBoxIP
		err = retry.OnError(
			backoff1min,
			func(err error) bool { return kubeerrors.IsNotFound(err) },
			func() error {
				netboxip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Get(context.Background(), netboxipName, metav1.GetOptions{})
				return err
			})
		if err != nil {
			t.Errorf("waiting for netboxip: %q", err)
		}

		expectedIP := &netbox.IPAddress{
			UID:     netbox.UID(netboxip.UID),
			DNSName: fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace),
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			Tags: []netbox.Tag{
				{Name: "kubernetes", Slug: "kubernetes"},
				{Name: "k8s-service", Slug: "k8s-service"},
			},
			Description: "app: foo",
		}

		if _, err := env.WaitForIP(expectedIP); err != nil {
			t.Fatal(err)
		}

		// update the service to not have ClusterIP, and make sure
		// the IP is deleted from NetBox
		svc.Spec.Type = corev1.ServiceTypeExternalName
		svc.Spec.ExternalName = "foo"
		svc.Spec.ClusterIP = ""
		_, err = env.KubeClient.CoreV1().Services(namespace).Update(context.Background(), svc, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating service: %q\n", err)
		}

		err = env.WaitForIPDeletion(netbox.UID(netboxip.UID))
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func testNetBoxIP(t *testing.T, env *testEnv) {
	namespace := "testnetboxip"

	testFunc := func() {
		ip := &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: namespace,
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: netip.AddrFrom4([4]byte{192, 168, 0, 1}),
				DNSName: "foo",
				Tags: []v1beta1.Tag{{
					Name: "kubernetes",
					Slug: "kubernetes",
				}},
				Description: "app: foo",
			},
		}

		// create
		var err error
		ip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Create(context.Background(), ip, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("creating netboxip: %q\n", err)
		}

		expectedIPInNetBox := &netbox.IPAddress{
			UID:     netbox.UID(ip.UID),
			DNSName: "foo",
			Address: netbox.IP(netip.AddrFrom4([4]byte{192, 168, 0, 1})),
			Tags: []netbox.Tag{
				{Name: "kubernetes", Slug: "kubernetes"},
			},
			Description: "app: foo",
		}

		if _, err := env.WaitForIP(expectedIPInNetBox); err != nil {
			t.Fatal(err)
		}

		// update
		ip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Get(context.Background(), ip.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("retrieving netboxip for update: %q\n", err)
		}

		ip.Spec.Tags = append(ip.Spec.Tags, v1beta1.Tag{Name: "k8s-pod", Slug: "k8s-pod"})
		ip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Update(context.Background(), ip, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating netboxip: %q\n", err)
		}

		expectedIPInNetBox.Tags = append(expectedIPInNetBox.Tags, netbox.Tag{Name: "k8s-pod", Slug: "k8s-pod"})

		if _, err = env.WaitForIP(expectedIPInNetBox); err != nil {
			t.Fatal(err)
		}

		// delete
		err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Delete(context.Background(), ip.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("deleting netboxip: %q\n", err)
		}

		err = env.WaitForIPDeletion(netbox.UID(ip.UID))
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func TestClean(t *testing.T) {
	env, err := newTestEnv()
	defer env.Stop()
	if err != nil {
		logger.Fatal("failed to start test env", zap.Error(err))
	}

	// create IPs to be cleaned up
	crdClient, err := crdregistration.NewClient(env.KubeConfig)
	if err != nil {
		t.Fatalf("creating registration client: %s", err)
	}

	if err := crdClient.Register(context.Background(), crd.NetBoxIPCRD); err != nil {
		t.Fatalf("registering CRD: %s", err)
	}

	var uids []netbox.UID
	for i := uint8(1); i < 3; i++ {
		addr := netip.AddrFrom4([4]byte{192, 168, 0, i})
		name := fmt.Sprintf("foo-%d", i)

		ip := &v1beta1.NetBoxIP{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NetBoxIP",
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  v1.NamespaceDefault,
				Finalizers: []string{netboxipctrl.IPFinalizer},
			},
			Spec: v1beta1.NetBoxIPSpec{
				Address: addr,
				DNSName: name,
			},
		}

		var err error
		ip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(v1.NamespaceDefault).Create(context.Background(), ip, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("creating netboxip: %q\n", err)
		}

		_, err = env.NetboxClient.UpsertIP(context.Background(), &netbox.IPAddress{
			UID:     netbox.UID(ip.UID),
			DNSName: name,
			Address: netbox.IP(addr),
		})
		if err != nil {
			t.Fatalf("pushing IP to NetBox")
		}

		uids = append(uids, netbox.UID(ip.UID))
	}

	// cleanup
	cfg := &globalConfig{
		netboxAPIURL: netboxAPIURL,
		netboxToken:  netboxToken,
		kubeConfig:   env.KubeConfig,
		netboxQPS:    rate.Inf,
		netboxBurst:  1,
		logger:       logger,
	}
	ctx := context.Background()
	if err := clean(ctx, cfg); err != nil {
		t.Error(err)
	}

	// check that IPs are removed from NetBox
	for _, uid := range uids {
		ip, err := env.NetboxClient.GetIP(ctx, uid)
		if err != nil {
			t.Errorf("unexpected error when checking if IP address exists: %v", err)
		} else if ip != nil {
			t.Errorf("want IP not to exist, got %v", ip)
		}
	}

	// check that netboxip CRD was removed
	extensionsClient, err := apiextensionsclient.NewForConfig(env.KubeConfig)
	if err != nil {
		t.Fatalf("creating API extensions client: %v", err)
	}

	_, err = extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.NetBoxIPCRDName, metav1.GetOptions{})
	if !kubeerrors.IsNotFound(err) {
		t.Errorf("want 'Not Found' error when retrieving netboxip CRD, got %v", err)
	}
}

func (env *testEnv) WithNamespace(namespace string, t *testing.T, f func()) {
	_, err := env.KubeClient.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})

	if err != nil {
		t.Errorf("creating namespace: %q\n", err)
		return
	}

	deleteNamespace := func() {
		err := env.KubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("deleting namespace: %q\n", err)
		}
	}
	defer deleteNamespace()

	f()
}

func (env *testEnv) WaitForIP(ip *netbox.IPAddress) (*netbox.IPAddress, error) {
	var foundIP *netbox.IPAddress
	notFoundErr := errors.New("IP not found")
	retryNotFound := func(err error) bool { return errors.Is(err, notFoundErr) }
	err := retry.OnError(backoff1min, retryNotFound, func() error {
		var err error
		foundIP, err = env.NetboxClient.GetIP(context.Background(), ip.UID)
		if err != nil {
			return err
		} else if foundIP == nil {
			return notFoundErr
		}

		diff := cmp.Diff(
			ip,
			foundIP,
			cmpopts.SortSlices(func(t1, t2 netbox.Tag) bool { return t1.Name < t2.Name }),
			cmpopts.IgnoreFields(netbox.IPAddress{}, "ID"),
			cmpopts.IgnoreFields(netbox.Tag{}, "ID"),
			cmpopts.IgnoreUnexported(netbox.IP{}),
			cmpopts.EquateEmpty(),
		)
		if diff != "" {
			return fmt.Errorf("%w:\n (-want, +got)\n%s", notFoundErr, diff)
		}

		return nil
	})
	return foundIP, err
}

func (env *testEnv) WaitForIPDeletion(uid netbox.UID) error {
	var ip *netbox.IPAddress
	foundErr := errors.New("IP still exists")
	retryFound := func(err error) bool { return err == foundErr }
	return retry.OnError(backoff1min, retryFound, func() error {
		var err error
		ip, err = env.NetboxClient.GetIP(context.Background(), uid)
		if err != nil {
			return err
		} else if ip != nil {
			return foundErr
		}
		return nil
	})
}

// A testEnv provides access to a test environment control plane.
type testEnv struct {
	KubeConfig    *rest.Config
	KubeClient    *kubernetes.Clientset
	KubeCRDClient *crdclient.Clientset
	NetboxClient  netbox.Client
	stop          func() error
}

// Stop stops the control plane.
func (env *testEnv) Stop() error {
	fmt.Println("stopping kubernetes control plane environment")
	if env.stop == nil {
		return nil
	}
	return env.stop()
}

// newTestEnv creates a new testEnv value. Callers are expected to call its
// Stop method.
func newTestEnv() (*testEnv, error) {
	env := envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			CleanUpAfterUse: true,
		},
		AttachControlPlaneOutput: testing.Verbose(),
		// DO NOT set ControlPlaneStartTimeout. Use KUBEBUILDER_CONTROLPLANE_START_TIMEOUT
		// to set it instead so it can vary by environment as needed.
	}

	if testing.Verbose() {
		os.Setenv("LOG_LEVEL", "debug")
	}

	apiserver := env.ControlPlane.GetAPIServer()
	apiserver.Configure().Set("service-cluster-ip-range", serviceCIDR)

	restConfig, err := env.Start()
	if err != nil {
		return nil, fmt.Errorf("starting envtest: %s", err)
	}

	stop := func() error {
		err := env.Stop()
		return err
	}

	restConfig.QPS = 50
	restConfig.Burst = 200
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		stop()
		return nil, fmt.Errorf("setting up kube client: %w", err)
	}

	kubeCRDClient, err := crdclient.NewForConfig(restConfig)
	if err != nil {
		stop()
		return nil, fmt.Errorf("setting up kube client for CRDs: %w", err)
	}

	netboxClient, err := netbox.NewClient(netboxAPIURL, netboxToken)
	if err != nil {
		stop()
		return nil, fmt.Errorf("setting up netbox client: %w", err)
	}

	return &testEnv{
		KubeConfig:    restConfig,
		KubeClient:    kubeClient,
		KubeCRDClient: kubeCRDClient,
		NetboxClient:  netboxClient,
		stop:          stop,
	}, nil
}

func newTestEnvWithController(ctx context.Context, t *testing.T) (*testEnv, error) {
	env, err := newTestEnv()
	if err != nil {
		return nil, err
	}

	globalCfg := &globalConfig{
		netboxAPIURL: netboxAPIURL,
		netboxToken:  netboxToken,
		kubeConfig:   env.KubeConfig,
		netboxQPS:    rate.Inf,
		netboxBurst:  1,
		logger:       logger,
	}
	cfg := &rootConfig{
		podTags:       []string{"kubernetes", "k8s-pod"},
		podLabels:     map[string]bool{"app": true},
		serviceTags:   []string{"kubernetes", "k8s-service"},
		serviceLabels: map[string]bool{"app": true},
		clusterDomain: "cluster.local",
	}
	go func() {
		defer env.Stop()
		if err := run(ctx, globalCfg, cfg); err != nil && err != context.Canceled {
			logger.Error("netbox-ip-controller stopped running", zap.Error(err))
		}
	}()

	return env, nil
}
