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
		t.Fatal(err)
	}

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

	pod, err = env.KubeClient.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating pod: %q\n", err)
	}
	t.Log("pod created")

	pod.Status = v1.PodStatus{
		PodIP: "172.17.0.1",
	}
	pod, err = env.KubeClient.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("updating pod: %q\n", err)
	}

	var ip *netbox.IPAddress
	notFoundErr := errors.New("IP not found")
	retryNotFound := func(err error) bool { return err == notFoundErr }
	err = retry.OnError(backoff1min, retryNotFound, func() error {
		ip, err = env.NetboxClient.GetIPByUID(context.Background(), string(pod.UID), pod.Name)
		if err != nil {
			return err
		} else if ip == nil {
			return notFoundErr
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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

	diff := cmp.Diff(
		expectedIP,
		ip,
		cmpopts.SortSlices(func(t1, t2 netbox.Tag) bool { return t1.Name < t2.Name }),
		cmpopts.IgnoreUnexported(netbox.IPAddress{}, netbox.Tag{}),
	)
	if diff != "" {
		t.Errorf("(-want, +got)\n%s", diff)
	}
}

// A testEnv provides access to a test environment control plane.
type testEnv struct {
	KubeClient   *kubernetes.Clientset
	NetboxClient netbox.Client
	stop         func() error
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
		return nil, fmt.Errorf("setting up kube client: %s", err)
	}

	netboxClient, err := netbox.NewClient(netboxAPIURL, netboxToken)
	if err != nil {
		stop()
		return nil, fmt.Errorf("setting up netbox client: %s", err)
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
		KubeClient:   kubeClient,
		NetboxClient: netboxClient,
		stop:         stop,
	}, nil
}
