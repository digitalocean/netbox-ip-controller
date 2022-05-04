package main

import (
	"context"
	"fmt"
	"time"

	netboxctrl "github.com/digitalocean/netbox-ip-controller"
	crd "github.com/digitalocean/netbox-ip-controller/api/netbox"
	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	log "go.uber.org/zap"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func newCleanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Removes all custom resources created by the controller, and all IPs created in NetBox.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := signals.SetupSignalHandler()
			return clean(ctx, globalCfg)
		},
	}
}

func clean(ctx context.Context, cfg *globalConfig) error {
	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		return err
	}
	kubeClient, err := client.New(cfg.kubeConfig, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating k8s client: %w", err)
	}

	netboxClient, err := netbox.NewClient(cfg.netboxAPIURL, cfg.netboxToken, cfg.netboxQPS, cfg.netboxBurst)
	if err != nil {
		return fmt.Errorf("creating netbox client: %w", err)
	}

	var netboxipList v1beta1.NetBoxIPList
	if err := kubeClient.List(ctx, &netboxipList); err != nil {
		return fmt.Errorf("listing netboxips: %w", err)
	}

	backoff1min := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1,
		Steps:    60,
	}

	var errs multierror.Error
	for _, ip := range netboxipList.Items {
		ll := log.L().With(log.String("uid", string(ip.UID)), log.Any("ip", ip.Spec.Address))

		err := retry.OnError(
			backoff1min,
			func(err error) bool { return true },
			func() error {
				if err := netboxClient.DeleteIP(ctx, netbox.UID(ip.UID)); err != nil {
					return fmt.Errorf("deleting IP from NetBox: %w", err)
				}
				ll.Info("deleted from NetBox")

				controllerutil.RemoveFinalizer(&ip, netboxctrl.IPFinalizer)
				if err := kubeClient.Update(ctx, &ip); err != nil {
					return fmt.Errorf("removing finalizer: %w", err)
				}
				if err := kubeClient.Delete(ctx, &ip); err != nil {
					return fmt.Errorf("deleting netboxip: %w", err)
				}
				ll.Info("netboxip deleted")
				return nil
			})
		if err != nil {
			multierror.Append(&errs, err)
		}
	}

	if errs.ErrorOrNil() != nil {
		return &errs
	}

	extensionsClient, err := apiextensionsclient.NewForConfig(cfg.kubeConfig)
	if err != nil {
		return fmt.Errorf("creating API extensions client: %w", err)
	}

	if err := extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.NetBoxIPCRDName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("deleting NetBoxIP custom resource: %w", err)
	}

	return nil
}
