package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	log "go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Controller is responsible for updating IPs of a single k8s resource.
type Controller interface {
	AddToManager(manager.Manager) error
}

// Settings specify configuration of a controller.
type Settings struct {
	NetboxClient netbox.Client
	Tags         []netbox.Tag
	Labels       map[string]bool
}

// Option can be used to tune controller settings.
type Option func(*Settings) error

// WithTags sets the tags that are applied to every IP
// published by the controller.
func WithTags(tags []string) Option {
	return func(s *Settings) error {
		if s.NetboxClient == nil {
			return errors.New("missing netbox client")
		}

		ctx := context.Background()
		for _, tag := range tags {
			existingTag, err := s.NetboxClient.GetTagByName(ctx, tag)
			if err != nil {
				return fmt.Errorf("retrieving tag %s: %w", tag, err)
			}

			ll := log.L().With(log.String("tag", tag))

			if existingTag != nil {
				ll.Info("tag already exists")
				s.Tags = append(s.Tags, *existingTag)
				continue
			}

			createdTag, err := s.NetboxClient.CreateTag(ctx, tag)
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
