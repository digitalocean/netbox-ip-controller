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
	svcctrl "github.com/digitalocean/netbox-ip-controller/internal/controller/service"
	"github.com/digitalocean/netbox-ip-controller/internal/crdregistration"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/go-logr/zapr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	log "go.uber.org/zap"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

const (
	flagMetricsAddr          = "metrics-addr"
	flagNetBoxAPIURL         = "netbox-api-url"
	flagNetBoxToken          = "netbox-token"
	flagKubeConfig           = "kube-config"
	flagKubeQPS              = "kube-qps"
	flagKubeBurst            = "kube-burst"
	flagNetBoxQPS            = "netbox-qps"
	flagNetBoxBurst          = "netbox-burst"
	flagPodIPTags            = "pod-ip-tags"
	flagServiceIPTags        = "service-ip-tags"
	flagPodPublishLabels     = "pod-publish-labels"
	flagServicePublishLabels = "service-publish-labels"
	flagClusterDomain        = "cluster-domain"
	flagDebug                = "debug"
	flagNetboxCACertPath     = "netbox-ca-cert-path"
)

type globalConfig struct {
	kubeConfig       *rest.Config
	netboxAPIURL     string
	netboxToken      string
	netboxQPS        rate.Limit
	netboxBurst      int
	logger           *log.Logger
	netboxCACertPath string
}

var globalCfg = &globalConfig{}

type rootConfig struct {
	metricsAddr     string
	podTags         []string
	serviceTags     []string
	podLabels       map[string]bool
	serviceLabels   map[string]bool
	clusterDomain   string
	rateLimitNetbox bool
}

func newRootCommand() *cobra.Command {
	cfg := &rootConfig{}

	cmd := &cobra.Command{
		Use:   "netbox-ip-controller --netbox-api-url <url> --netbox-token <token>",
		Short: "Start netbox-ip-controller: watch pods and services, and publish their IPs to NetBox.",
		Long: `
Netbox-ip-controller publishes IPs of Kubernetes objects to NetBox.
It registers a NetBoxIP custom resource, and uses it to store IPs of pods and services
to be published.`,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return globalCfg.setup(cmd)
		},
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return cfg.setup(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := signals.SetupSignalHandler()
			return run(ctx, globalCfg, cfg)
		},
	}

	registerGlobalFlags(cmd)
	registerRootFlags(cmd)

	return cmd
}

// register global flags inherited by all children commands
func registerGlobalFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagNetBoxAPIURL, "", "URL of the NetBox API server to connect to (scheme://host:port/path)")
	cmd.PersistentFlags().String(flagNetBoxToken, "", "NetBox API token to use for authentication")
	cmd.PersistentFlags().String(flagKubeConfig, "", "absolute path to the kubeconfig file specifying the kube-apiserver instance; leave empty if the controller is running in-cluster")
	cmd.PersistentFlags().Float64(flagKubeQPS, 20.0, "maximum number of requests per second to the kube-apiserver")
	cmd.PersistentFlags().Int(flagKubeBurst, 30, "maximum number of requests to the kube-apiserver allowed to accumulate before throttling begins")
	cmd.PersistentFlags().Float64(flagNetBoxQPS, 100.0, "average allowable requests per second to NetBox API, i.e., the rate limiter's token bucket refill rate per second")
	cmd.PersistentFlags().Int(flagNetBoxBurst, 1, "maximum allowable burst of requests to NetBox API, i.e. the rate limiter's token bucket size")
	cmd.PersistentFlags().Bool(flagDebug, false, "turn on debug logging")
	cmd.PersistentFlags().String(flagNetboxCACertPath, "", "absolute path to a file containing a PEM-encoded root certificate to verify certificates signed by Netbox's CA")
}

// register flags relevant for the root command itself, but not its children
func registerRootFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagMetricsAddr, ":8001", "the port on which to serve metrics")
	cmd.Flags().String(flagPodIPTags, "kubernetes,k8s-pod", "comma-separated list of tags to add to pod IPs in NetBox")
	cmd.Flags().String(flagServiceIPTags, "kubernetes,k8s-service", "comma-separated list of tags to add to service IPs in NetBox")
	cmd.Flags().String(flagPodPublishLabels, "app", "comma-separated list of pod labels that should be added to the IP description in NetBox")
	cmd.Flags().String(flagServicePublishLabels, "app", "comma-separated list of service labels that should be added to the IP description in NetBox")
	cmd.Flags().String(flagClusterDomain, "cluster.local", "domain name of the cluster")

}

func (cfg *globalConfig) setup(cmd *cobra.Command) error {
	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
		return fmt.Errorf("binding flags: %w", err)
	}

	cfg.netboxAPIURL = v.GetString(flagNetBoxAPIURL)

	cfg.netboxToken = v.GetString(flagNetBoxToken)

	kubeConfigFile := v.GetString(flagKubeConfig)

	kubeConfig, err := kubeConfig(kubeConfigFile)
	if err != nil {
		return fmt.Errorf("failed to setup k8s client config: %s", err)
	}
	cfg.kubeConfig = kubeConfig
	cfg.kubeConfig.QPS = float32(v.GetFloat64(flagKubeQPS))
	cfg.kubeConfig.Burst = v.GetInt(flagKubeBurst)
	cfg.netboxQPS = rate.Limit(v.GetFloat64(flagNetBoxQPS))
	cfg.netboxBurst = v.GetInt(flagNetBoxBurst)
	cfg.netboxCACertPath = v.GetString(flagNetboxCACertPath)

	err = cfg.validate()
	if err != nil {
		return err
	}

	logger, err := log.NewProduction()
	if v.GetBool(flagDebug) {
		logger, err = log.NewDevelopment()
	}
	if err != nil {
		return fmt.Errorf("cannot initialize logger: %w", err)
	}
	cfg.logger = logger

	return nil
}

func (cfg *globalConfig) validate() error {
	if cfg.netboxAPIURL == "" {
		return fmt.Errorf("%s was not provided", flagNetBoxAPIURL)
	}
	if cfg.netboxToken == "" {
		return fmt.Errorf("%s was not provided", flagNetBoxToken)
	}
	if cfg.netboxQPS <= 0 {
		return fmt.Errorf("%s value %f is invalid: must be greater than 0", flagNetBoxQPS, cfg.netboxQPS)
	}
	if cfg.netboxBurst < 1 {
		return fmt.Errorf("%s value %d is invalid: must be at least 1", flagNetBoxBurst, cfg.netboxBurst)
	}
	return nil
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

	rc.UserAgent = "netbox-ip-controller"

	return rc, nil
}

func (cfg *rootConfig) setup(cmd *cobra.Command) error {
	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return fmt.Errorf("binding flags: %w", err)
	}

	cfg.metricsAddr = v.GetString(flagMetricsAddr)
	cfg.clusterDomain = v.GetString(flagClusterDomain)

	cfg.podTags = sanitizedStringSlice(v.GetString(flagPodIPTags))
	cfg.serviceTags = sanitizedStringSlice(v.GetString(flagServiceIPTags))

	cfg.podLabels = make(map[string]bool)
	for _, l := range sanitizedStringSlice(v.GetString(flagPodPublishLabels)) {
		cfg.podLabels[l] = true
	}
	cfg.serviceLabels = make(map[string]bool)
	for _, l := range sanitizedStringSlice(v.GetString(flagServicePublishLabels)) {
		cfg.serviceLabels[l] = true
	}

	err := cfg.validate()
	if err != nil {
		return err
	}

	return nil
}

func (cfg *rootConfig) validate() error {
	for l := range cfg.serviceLabels {
		err := validateLabel(l)
		if err != nil {
			return fmt.Errorf("%s value %q is not a valid kubernetes label: %w", flagServicePublishLabels, l, err)
		}
	}
	for l := range cfg.podLabels {
		err := validateLabel(l)
		if err != nil {
			return fmt.Errorf("%s value %q is not a valid kubernetes label: %w", flagPodPublishLabels, l, err)
		}
	}
	return nil
}

// stringSlice splits a comma-separated list of values into a slice of strings
// NOTE: cannot use viper.GetStringSlice(key) b/c it doesn't parse comma-separated env vars
// correctly: https://github.com/spf13/viper/issues/380
func sanitizedStringSlice(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}

	values := strings.Split(s, ",")
	var sanitized []string
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed != "" {
			sanitized = append(sanitized, trimmed)
		}
	}
	return sanitized
}

// validateLabel returns a nil error if s is a valid kubernetes label value,
// else it returns an error containing the reason(s) it is not valid
func validateLabel(s string) error {
	stringErrs := validation.IsQualifiedName(s)
	if stringErrs != nil {
		return fmt.Errorf("%v", stringErrs)
	}
	return nil
}

func run(ctx context.Context, globalCfg *globalConfig, cfg *rootConfig) error {
	logger := globalCfg.logger
	defer logger.Sync()

	clientOpts := []netbox.ClientOption{
		netbox.WithRateLimiter(globalCfg.netboxQPS, globalCfg.netboxBurst),
		netbox.WithLogger(logger),
	}
	if globalCfg.netboxCACertPath != "" {
		clientOpts = append(clientOpts, netbox.WithCARootCert(globalCfg.netboxCACertPath))
	}
	netboxClient, err := netbox.NewClient(globalCfg.netboxAPIURL, globalCfg.netboxToken, clientOpts...)
	if err != nil {
		return err
	}

	crdClient, err := crdregistration.NewClient(globalCfg.kubeConfig)
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

	mgr, err := manager.New(globalCfg.kubeConfig, manager.Options{
		Scheme:             scheme,
		Logger:             zapr.NewLogger(logger.Named("netbox-ip-controller")),
		MetricsBindAddress: cfg.metricsAddr,
	})
	if err != nil {
		return fmt.Errorf("unable to set up manager: %s", err)
	}
	logger.Info("created manager")

	controllers := make(map[string]ctrl.Controller)

	netboxController, err := netboxipctrl.New(ctrl.WithNetBoxClient(netboxClient))
	if err != nil {
		return fmt.Errorf("initializing netbox controller: %q", err)
	}
	controllers["netboxip"] = netboxController

	podController, err := podctrl.New(
		ctrl.WithLogger(logger),
		ctrl.WithTags(cfg.podTags, netboxClient),
		ctrl.WithLabels(cfg.podLabels),
	)
	if err != nil {
		return fmt.Errorf("initializing pod controller: %s", err)
	}
	controllers["pod"] = podController

	svcController, err := svcctrl.New(
		ctrl.WithLogger(logger),
		ctrl.WithTags(cfg.serviceTags, netboxClient),
		ctrl.WithLabels(cfg.serviceLabels),
		ctrl.WithClusterDomain(cfg.clusterDomain),
	)
	if err != nil {
		return fmt.Errorf("initializing service controller: %s", err)
	}
	controllers["service"] = svcController

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
