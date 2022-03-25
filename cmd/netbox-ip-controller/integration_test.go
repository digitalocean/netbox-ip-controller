//go:build sandbox
// +build sandbox

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	crdclient "github.com/digitalocean/netbox-ip-controller/client/clientset/versioned"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	log "go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	netboxAPIURL = "http://netbox:8080/api"
	netboxToken  = "48c7ba92-0f82-443a-8cf3-981559ff32cf"
	env          *testEnv
	backoff1min  = wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   1,
		Steps:    20,
	}
)

func TestMain(m *testing.M) {
	// need to have -v flag parsed before setting up envtest
	flag.Parse()

	// start a test cluster with envtest
	ctx, cancel := context.WithCancel(context.Background())
	var err error
	env, err = newTestEnv(ctx)
	if err != nil {
		log.L().Fatal("failed to start test env", log.Error(err))
	}

	exitCode := m.Run()

	env.Stop()
	cancel()

	os.Exit(exitCode)
}

// TODO(dasha): look into using kind for testing.
// Current envtest setup, does not include kube-controller-manager,
// so, while pods, services etc. can be created, they are not
// reconciled.

func TestPodUpdate(t *testing.T) {
	namespace := "foo"

	testFunc := func() {
		pod := &v1.Pod{
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
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
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

		pod.Status = v1.PodStatus{
			PodIP: "172.17.0.1",
		}
		pod, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating pod: %q\n", err)
		}

		expectedIP := &netbox.IPAddress{
			UID:     string(pod.UID),
			DNSName: pod.Name,
			Address: net.IPv4(172, 17, 0, 1),
			Tags: []netbox.Tag{
				{Name: "kubernetes", Slug: "kubernetes"},
				{Name: "pod", Slug: "pod"},
			},
			Description: "app: foo",
		}

		if _, err := env.WaitForIP(expectedIP); err != nil {
			t.Fatal(err)
		}

		// delete IP from pod status and expect the IP to be removed from NetBox
		pod, err = env.KubeClient.CoreV1().Pods(namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("retrieving current pod: %q\n", err)
		}
		pod.Status = v1.PodStatus{}
		_, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating pod: %q\n", err)
		}

		ipKey := netbox.IPAddressKey{UID: string(pod.UID), DNSName: pod.Name}
		err = env.WaitForIPDeletion(ipKey)
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func TestPodDelete(t *testing.T) {
	namespace := "bar"

	testFunc := func() {
		pod := &v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: namespace,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{
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

		pod.Status = v1.PodStatus{
			PodIP: "172.17.0.1",
		}
		_, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating pod status: %q\n", err)
		}

		expectedIP := &netbox.IPAddress{
			UID:     string(pod.UID),
			DNSName: pod.Name,
			Address: net.IPv4(172, 17, 0, 1),
			Tags: []netbox.Tag{
				{Name: "kubernetes", Slug: "kubernetes"},
				{Name: "pod", Slug: "pod"},
			},
		}
		_, err = env.WaitForIP(expectedIP)
		if err != nil {
			t.Fatal(err)
		}

		// now delete the pod and expect the IP to be removed from NetBox
		err = env.KubeClient.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("deleting pod: %q\n", err)
		}

		ipKey := netbox.IPAddressKey{DNSName: pod.Name, UID: string(pod.UID)}
		err = env.WaitForIPDeletion(ipKey)
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func TestNetBoxIP(t *testing.T) {
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
				Address: v1beta1.IP(net.IPv4(192, 168, 0, 1)),
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
			UID:     string(ip.UID),
			DNSName: "foo",
			Address: net.IPv4(192, 168, 0, 1),
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

		ip.Spec.Tags = append(ip.Spec.Tags, v1beta1.Tag{Name: "pod", Slug: "pod"})
		ip, err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Update(context.Background(), ip, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("updating netboxip: %q\n", err)
		}

		expectedIPInNetBox.Tags = append(expectedIPInNetBox.Tags, netbox.Tag{Name: "pod", Slug: "pod"})

		if _, err = env.WaitForIP(expectedIPInNetBox); err != nil {
			t.Fatal(err)
		}

		// delete
		err = env.KubeCRDClient.NetboxV1beta1().NetBoxIPs(namespace).Delete(context.Background(), ip.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("deleting netboxip: %q\n", err)
		}

		ipKey := netbox.IPAddressKey{
			DNSName: ip.Spec.DNSName,
			UID:     string(ip.UID),
		}
		err = env.WaitForIPDeletion(ipKey)
		if err != nil {
			t.Error(err)
		}
	}

	env.WithNamespace(namespace, t, testFunc)
}

func (env *testEnv) WithNamespace(namespace string, t *testing.T, f func()) {
	_, err := env.KubeClient.CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
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
	key := netbox.IPAddressKey{UID: ip.UID, DNSName: ip.DNSName}

	var foundIP *netbox.IPAddress
	notFoundErr := errors.New("IP not found")
	retryNotFound := func(err error) bool { return errors.Is(err, notFoundErr) }
	err := retry.OnError(backoff1min, retryNotFound, func() error {
		var err error
		foundIP, err = env.NetboxClient.GetIP(context.Background(), key)
		if err != nil {
			return err
		} else if foundIP == nil {
			return notFoundErr
		}

		diff := cmp.Diff(
			ip,
			foundIP,
			cmpopts.SortSlices(func(t1, t2 netbox.Tag) bool { return t1.Name < t2.Name }),
			cmpopts.IgnoreUnexported(netbox.IPAddress{}, netbox.Tag{}),
		)
		if diff != "" {
			return fmt.Errorf("%w:\n (-want, +got)\n%s", notFoundErr, diff)
		}

		return nil
	})
	return foundIP, err
}

func (env *testEnv) WaitForIPDeletion(key netbox.IPAddressKey) error {
	var ip *netbox.IPAddress
	foundErr := errors.New("IP still exists")
	retryFound := func(err error) bool { return err == foundErr }
	return retry.OnError(backoff1min, retryFound, func() error {
		var err error
		ip, err = env.NetboxClient.GetIP(context.Background(), key)
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
	KubeClient    *kubernetes.Clientset
	KubeCRDClient *crdclient.Clientset
	NetboxClient  netbox.Client
	stop          func() error
}

// Stop stops the control plane.
func (env *testEnv) Stop() error {
	if env.stop == nil {
		return nil
	}

	return env.stop()
}

// newTestEnv creates a new testEnv value. Callers are expected to call its
// Stop method.
func newTestEnv(ctx context.Context) (*testEnv, error) {
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
	apiserver.Configure()

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
		return nil, fmt.Errorf("setting up kube client for CRDs: %w", err)
	}

	netboxClient, err := netbox.NewClient(netboxAPIURL, netboxToken)
	if err != nil {
		stop()
		return nil, fmt.Errorf("setting up netbox client: %w", err)
	}

	if err := netboxClient.CreateUIDField(ctx); err != nil {
		return nil, fmt.Errorf("creating UID field: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	cfg := &config{
		netboxAPIURL: netboxAPIURL,
		netboxToken:  netboxToken,
		kubeConfig:   restConfig,
		podTags:      []string{"kubernetes", "pod"},
		podLabels:    map[string]bool{"app": true},
	}
	go func() {
		if err := realMain(ctx, cfg); err != nil && err != context.Canceled {
			log.L().Error("netbox-ip-controller stopped running", log.Error(err))
			stop()
			cancel()
		}
	}()

	return &testEnv{
		KubeClient:    kubeClient,
		KubeCRDClient: kubeCRDClient,
		NetboxClient:  netboxClient,
		stop:          stop,
	}, nil
}
