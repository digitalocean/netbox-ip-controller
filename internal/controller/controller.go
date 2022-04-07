package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Controller is responsible for updating IPs of a single k8s resource.
type Controller interface {
	AddToManager(manager.Manager) error
}

// Settings specify configuration of a controller.
type Settings struct {
	NetBoxClient  netbox.Client
	Tags          []netbox.Tag
	Labels        map[string]bool
	ClusterDomain string
}

// Option can be used to tune controller settings.
type Option func(*Settings) error

// WithTags sets the tags that are applied to every IP
// published by the controller.
func WithTags(tags []string, netboxClient netbox.Client) Option {
	return func(s *Settings) error {
		s.NetBoxClient = netboxClient

		if s.NetBoxClient == nil {
			return errors.New("missing netbox client")
		}

		ctx := context.Background()
		for _, tag := range tags {
			existingTag, err := s.NetBoxClient.GetTag(ctx, tag)
			if err != nil {
				return fmt.Errorf("retrieving tag %s: %w", tag, err)
			}

			ll := log.L().With(log.String("tag", tag))

			if existingTag != nil {
				ll.Info("tag already exists")
				s.Tags = append(s.Tags, *existingTag)
				continue
			}

			createdTag, err := s.NetBoxClient.CreateTag(ctx, tag)
			if err != nil {
				return fmt.Errorf("creating tag %s: %w", tag, err)
			}
			s.Tags = append(s.Tags, *createdTag)
			ll.Info("created tag")
		}
		return nil
	}
}

// WithLabels sets the k8s object labels that are added to the description
// of every IP published by the controller.
func WithLabels(labels map[string]bool) Option {
	return func(s *Settings) error {
		s.Labels = labels
		return nil
	}
}

// WithNetBoxClient sets the NetBox client to be used by the controller.
func WithNetBoxClient(client netbox.Client) Option {
	return func(s *Settings) error {
		s.NetBoxClient = client
		return nil
	}
}

// WithClusterDomain sets the k8s cluster domain name.
func WithClusterDomain(domain string) Option {
	return func(s *Settings) error {
		s.ClusterDomain = domain
		return nil
	}
}

// OnCreateAndUpdateFilter is an event filter that keeps
// only create and update events, and not deletes.
var OnCreateAndUpdateFilter = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return e.Object != nil
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectNew != nil
	},
	DeleteFunc: func(_ event.DeleteEvent) bool {
		return false
	},
}
