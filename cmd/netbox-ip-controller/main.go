package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	podctrl "github.com/digitalocean/netbox-ip-controller/internal/controller/pod"

	"github.com/go-logr/zapr"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/ianschenck/envflag"
	netbox "github.com/netbox-community/go-netbox/netbox/client"
	log "go.uber.org/zap"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

const name = "netbox-ip-controller"

type config struct {
	metricsAddr  string
	kubeConfig   *rest.Config
	netboxAPIURL string
	netboxToken  string
}

func main() {
	cfg, err := setupConfig()
	if err != nil {
		log.L().Fatal("setting up config", log.Error(err))
	}

	ctx := signals.SetupSignalHandler()

	if err := realMain(ctx, cfg); err != nil {
		log.L().Fatal("controller exited", log.Error(err))
	}
}

func realMain(ctx context.Context, cfg *config) error {
	u, err := url.Parse(cfg.netboxAPIURL)
	if err != nil {
		return fmt.Errorf("failed to parse NetBox URL: %s", err)
	} else if !u.IsAbs() {
		return errors.New("NetBox URL must be in scheme://host:port format")
	}

	transport := httptransport.New(u.Hostname(), u.Path, []string{u.Scheme})
	transport.DefaultAuthentication = httptransport.APIKeyAuth("Authorization", "header", "Token "+cfg.netboxToken)
	transport.SetDebug(true)

	netboxClient := netbox.New(transport, nil)

	mgr, err := manager.New(cfg.kubeConfig, manager.Options{
		Logger:             zapr.NewLogger(log.L().Named(name)),
		MetricsBindAddress: cfg.metricsAddr,
	})
	if err != nil {
		return fmt.Errorf("unable to set up manager: %s", err)
	}
	log.L().Info("created manager")

	if err := podctrl.New(netboxClient).AddToManager(mgr); err != nil {
		return fmt.Errorf("could not create controller for pod IPs: %s", err)
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("could not start manager: %s", err)
	}

	return nil
}

func setupConfig() (*config, error) {
	var cfg config
	var kubeConfigFile string
	var kubeQPS float64
	var kubeBurst int

	envflag.StringVar(&cfg.metricsAddr, "METRICS_ADDR", "8001", "the port on which to serve metrics")
	envflag.StringVar(&cfg.netboxAPIURL, "NETBOX_API_URL", "", "URL of the NetBox API server to connect to (scheme://host:port/path)")
	envflag.StringVar(&cfg.netboxToken, "NETBOX_TOKEN", "", "NetBox API token to use for authentication")
	envflag.StringVar(&kubeConfigFile, "KUBE_CONFIG", "", "absolute path to the kubeconfig file specifying the kube-apiserver instance; leave empty if the controller is running in-cluster")
	envflag.Float64Var(&kubeQPS, "KUBE_QPS", 20.0, "maximum number of requests per second to the kube-apiserver")
	envflag.IntVar(&kubeBurst, "KUBE_BURST", 30, "maximum number of requests to the kube-apiserver allowed to accumulate before throttling begins")

	envflag.Parse()

	kubeConfig, err := kubeConfig(kubeConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to setup k8s client config: %s", err)
	}
	cfg.kubeConfig = kubeConfig
	cfg.kubeConfig.QPS = float32(kubeQPS)
	cfg.kubeConfig.Burst = kubeBurst

	return &cfg, nil
}

func kubeConfig(kubeconfigFile string) (*rest.Config, error) {
	var rc *rest.Config
	var err error
	if kubeconfigFile != "" {
		if rc, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile); err != nil {
			return nil, err
		}
	} else {
		if rc, err = rest.InClusterConfig(); err != nil {
			return nil, err
		}
	}

	rc.UserAgent = name

	return rc, nil
}
