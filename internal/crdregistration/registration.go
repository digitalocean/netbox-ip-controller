package crdregistration

import (
	"context"
	"errors"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

// Client is a client for registering custom resources.
type Client struct {
	apiextensionsclient apiextensionsclient.Interface
}

// NewClient returns a client that connects to the API server specified by the kubeConfig provided.
func NewClient(kubeConfig *rest.Config) (*Client, error) {
	c, err := apiextensionsclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &Client{apiextensionsclient: c}, nil
}

// Register upserts a CustomResourceDefinition.
func (c *Client) Register(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existingCRD, err := c.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})

		if kubeerrors.IsNotFound(err) {
			_, err = c.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("creating CRD: %w", err)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("retrieving existing CRD: %w", err)
		}

		existingCRD.Spec = crd.Spec
		_, err = c.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, existingCRD, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating CRD: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := c.wait(ctx, crd.Name); err != nil {
		return fmt.Errorf("waiting for CRD to be established: %w", err)
	}
	return nil
}

func (c Client) wait(ctx context.Context, name string) error {
	backoff1min := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1,
		Steps:    60,
	}

	notReadyErr := fmt.Errorf("CRD not ready")

	return retry.OnError(
		backoff1min,
		func(err error) bool { return errors.Is(err, notReadyErr) },
		func() error {
			crd, err := c.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("%w: %s", notReadyErr, err)
			}

			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1.Established:
					if cond.Status == apiextensionsv1.ConditionTrue {
						return nil
					}
				case apiextensionsv1.NamesAccepted:
					if cond.Status == apiextensionsv1.ConditionFalse {
						return fmt.Errorf("name conflict: %v", cond.Reason)
					}
				}
			}

			// no "Established" condition
			return notReadyErr
		})
}
