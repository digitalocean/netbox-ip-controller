package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"

	log "go.uber.org/zap"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// NetBoxIPName derives NetBoxIP name from the object's metadata.
func NetBoxIPName(obj client.Object) string {
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	// use UIDs instead of names in case of name conflicts
	return fmt.Sprintf("%s-%s", strings.ToLower(kind), obj.GetUID())
}

// DeclareOwner sets the provided object as the controller of
// the given NetBoxIP.
func DeclareOwner(ip *v1beta1.NetBoxIP, obj client.Object) error {
	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("creating owner scheme: %w", err)
	}

	if err := controllerutil.SetControllerReference(obj, ip, scheme); err != nil {
		return fmt.Errorf("could not set owner: %w", err)
	}
	return nil
}

// UpsertNetBoxIP creates or updates (if exists) the NetBoxIP provided.
func UpsertNetBoxIP(ctx context.Context, kubeClient client.Client, ll *log.Logger, ip *v1beta1.NetBoxIP) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existingIP v1beta1.NetBoxIP
		err := kubeClient.Get(ctx, client.ObjectKey{Namespace: ip.Namespace, Name: ip.Name}, &existingIP)
		if kubeerrors.IsNotFound(err) {
			if err := kubeClient.Create(ctx, ip); err != nil {
				return fmt.Errorf("creating netboxip: %w", err)
			}
			ll.Info("created netboxip")
			return nil
		} else if err != nil {
			return fmt.Errorf("retrieving netboxip: %w", err)
		}

		if !ip.Spec.Changed(existingIP.Spec) {
			return nil
		}

		existingIP.Spec = ip.Spec
		existingIP.OwnerReferences = ip.OwnerReferences
		existingIP.Labels = ip.Labels
		if err := kubeClient.Update(ctx, &existingIP); err != nil {
			return fmt.Errorf("updating netboxip: %w", err)
		}
		ll.Info("updated netboxip")

		return nil
	})
}
