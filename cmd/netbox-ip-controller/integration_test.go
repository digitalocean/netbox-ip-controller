//go:build sandbox
// +build sandbox

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	log "go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	netboxPort   int32 = 31111
	netboxAPIURL       = fmt.Sprintf("http://localhost:%d/api", netboxPort)
	netboxToken        = "48c7ba92-0f82-443a-8cf3-981559ff32cf"
)

func TestMain(m *testing.M) {
	// need to have -v flag parsed before setting up envtest
	flag.Parse()

	// start a test cluster with envtest
	ctx, cancel := context.WithCancel(context.Background())
	env, err := newTestEnv(ctx)
	if err != nil {
		log.L().Fatal("failed to start test env", log.Error(err))
	}

	exitCode := m.Run()

	env.Stop()
	cancel()

	os.Exit(exitCode)
}

func TestPlaceholder(t *testing.T) {
	t.Log("some test here")
}

// A testEnv provides access to a test environment control plane.
type testEnv struct {
	KubeConfig *rest.Config
	KubeClient *kubernetes.Clientset
	stop       func() error
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

	ctx, cancel := context.WithCancel(ctx)
	cfg := &config{
		netboxAPIURL: netboxAPIURL,
		netboxToken:  netboxToken,
		kubeConfig:   restConfig,
	}
	go func() {
		if err := realMain(ctx, cfg); err != nil && err != context.Canceled {
			log.L().Error("netbox-ip-controller stopped running", log.Error(err))
			stop()
			cancel()
		}
	}()

	return &testEnv{
		KubeClient: kubeClient,
		stop:       stop,
	}, nil
}
