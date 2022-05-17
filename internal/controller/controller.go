/*
Copyright 2022 DigitalOcean

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at:

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	Logger        *log.Logger
}

// Option can be used to tune controller settings.
type Option func(*Settings) error

// WithLogger sets the logger to be used by the controller.
func WithLogger(logger *log.Logger) Option {
	return func(s *Settings) error {
		s.Logger = logger
		return nil
	}
}

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
			if s.Logger != nil {
				ll = s.Logger.With(log.String("tag", tag))
			}

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
