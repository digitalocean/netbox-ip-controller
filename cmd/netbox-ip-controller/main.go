package main

import (
	"context"
	"fmt"
	"strings"

	crd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	ctrl "github.com/digitalocean/netbox-ip-controller/internal/controller"
	netboxipctrl "github.com/digitalocean/netbox-ip-controller/internal/controller/netbox-ip"
	podctrl "github.com/digitalocean/netbox-ip-controller/internal/controller/pod"
	"github.com/digitalocean/netbox-ip-controller/internal/crdregistration"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/go-logr/zapr"
	"github.com/ianschenck/envflag"
	log "go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

const name = "netbox-ip-controller"

type config struct {
	metricsAddr   string
	kubeConfig    *rest.Config
	netboxAPIURL  string
	netboxToken   string
	podTags       []string
	serviceTags   []string
	podLabels     map[string]bool
	serviceLabels map[string]bool
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
	netboxClient, err := netbox.NewClient(cfg.netboxAPIURL, cfg.netboxToken)
	if err != nil {
		return err
	}

	crdClient, err := crdregistration.NewClient(cfg.kubeConfig)
	if err != nil {
		return err
	}

	if err := crdClient.Register(ctx, crd.NetBoxIPCRD); err != nil {
		return err
	}

	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := v1beta1.AddToScheme(scheme); err != nil {
		return err
	}

	mgr, err := manager.New(cfg.kubeConfig, manager.Options{
		Scheme:             scheme,
		Logger:             zapr.NewLogger(log.L().Named(name)),
		MetricsBindAddress: cfg.metricsAddr,
	})
	if err != nil {
		return fmt.Errorf("unable to set up manager: %s", err)
	}
	log.L().Info("created manager")

	controllers := make(map[string]ctrl.Controller)

	netboxController, err := netboxipctrl.New(netboxClient)
	if err != nil {
		return fmt.Errorf("initializing netbox controller: %q", err)
	}
	controllers["netboxip"] = netboxController

	podController, err := podctrl.New(netboxClient, ctrl.WithTags(cfg.podTags), ctrl.WithLabels(cfg.podLabels))
	if err != nil {
		return fmt.Errorf("initializing pod controller: %s", err)
	}
	controllers["pod"] = podController

	for name, controller := range controllers {
		if err := controller.AddToManager(mgr); err != nil {
			return fmt.Errorf("could not create %s controller: %s", name, err)
		}
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("could not start manager: %s", err)
	}

	return nil
}

func setupConfig() (*config, error) {
	var (
		cfg              config
		kubeConfigFile   string
		kubeQPS          float64
		kubeBurst        int
		podTagsStr       string
		serviceTagsStr   string
		podLabelsStr     string
		serviceLabelsStr string
	)

	envflag.StringVar(&cfg.metricsAddr, "METRICS_ADDR", "8001", "the port on which to serve metrics")
	envflag.StringVar(&cfg.netboxAPIURL, "NETBOX_API_URL", "", "URL of the NetBox API server to connect to (scheme://host:port/path)")
	envflag.StringVar(&cfg.netboxToken, "NETBOX_TOKEN", "", "NetBox API token to use for authentication")
	envflag.StringVar(&kubeConfigFile, "KUBE_CONFIG", "", "absolute path to the kubeconfig file specifying the kube-apiserver instance; leave empty if the controller is running in-cluster")
	envflag.Float64Var(&kubeQPS, "KUBE_QPS", 20.0, "maximum number of requests per second to the kube-apiserver")
	envflag.IntVar(&kubeBurst, "KUBE_BURST", 30, "maximum number of requests to the kube-apiserver allowed to accumulate before throttling begins")
	envflag.StringVar(&podTagsStr, "POD_IP_TAGS", "kubernetes,pod", "comma-separated list of tags to add to pod IPs in NetBox")
	envflag.StringVar(&serviceTagsStr, "SERVICE_IP_TAGS", "kubernetes,service", "comma-separated list of tags to add to service IPs in NetBox")
	envflag.StringVar(&podLabelsStr, "POD_PUBLISH_LABELS", "app", "comma-separated list of pod labels that should be added to the IP description in NetBox")
	envflag.StringVar(&serviceLabelsStr, "SERVICE_PUBLISH_LABELS", "app", "comma-separated list of service labels that should be added to the IP description in NetBox")

	envflag.Parse()

	// TODO(dasha): validation for flags

	kubeConfig, err := kubeConfig(kubeConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to setup k8s client config: %s", err)
	}
	cfg.kubeConfig = kubeConfig
	cfg.kubeConfig.QPS = float32(kubeQPS)
	cfg.kubeConfig.Burst = kubeBurst

	// TODO(dasha): maybe trim spaces around those tags?
	cfg.podTags = strings.Split(podTagsStr, ",")
	cfg.serviceTags = strings.Split(serviceTagsStr, ",")

	cfg.podLabels = make(map[string]bool)
	cfg.serviceLabels = make(map[string]bool)
	for _, l := range strings.Split(podLabelsStr, ",") {
		cfg.podLabels[l] = true
	}
	for _, l := range strings.Split(serviceLabelsStr, ",") {
		cfg.serviceLabels[l] = true
	}

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
