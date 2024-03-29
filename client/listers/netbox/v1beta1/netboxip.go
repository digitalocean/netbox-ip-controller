/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1beta1

import (
	v1beta1 "github.com/digitalocean/netbox-ip-controller/api/netbox/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NetBoxIPLister helps list NetBoxIPs.
// All objects returned here must be treated as read-only.
type NetBoxIPLister interface {
	// List lists all NetBoxIPs in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.NetBoxIP, err error)
	// NetBoxIPs returns an object that can list and get NetBoxIPs.
	NetBoxIPs(namespace string) NetBoxIPNamespaceLister
	NetBoxIPListerExpansion
}

// netBoxIPLister implements the NetBoxIPLister interface.
type netBoxIPLister struct {
	indexer cache.Indexer
}

// NewNetBoxIPLister returns a new NetBoxIPLister.
func NewNetBoxIPLister(indexer cache.Indexer) NetBoxIPLister {
	return &netBoxIPLister{indexer: indexer}
}

// List lists all NetBoxIPs in the indexer.
func (s *netBoxIPLister) List(selector labels.Selector) (ret []*v1beta1.NetBoxIP, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.NetBoxIP))
	})
	return ret, err
}

// NetBoxIPs returns an object that can list and get NetBoxIPs.
func (s *netBoxIPLister) NetBoxIPs(namespace string) NetBoxIPNamespaceLister {
	return netBoxIPNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// NetBoxIPNamespaceLister helps list and get NetBoxIPs.
// All objects returned here must be treated as read-only.
type NetBoxIPNamespaceLister interface {
	// List lists all NetBoxIPs in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.NetBoxIP, err error)
	// Get retrieves the NetBoxIP from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1beta1.NetBoxIP, error)
	NetBoxIPNamespaceListerExpansion
}

// netBoxIPNamespaceLister implements the NetBoxIPNamespaceLister
// interface.
type netBoxIPNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all NetBoxIPs in the indexer for a given namespace.
func (s netBoxIPNamespaceLister) List(selector labels.Selector) (ret []*v1beta1.NetBoxIP, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.NetBoxIP))
	})
	return ret, err
}

// Get retrieves the NetBoxIP from the indexer for a given namespace and name.
func (s netBoxIPNamespaceLister) Get(name string) (*v1beta1.NetBoxIP, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1beta1.Resource("netboxip"), name)
	}
	return obj.(*v1beta1.NetBoxIP), nil
}
