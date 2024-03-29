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

// Package metrics contains all custom metrics to be exported to prometheus
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	kubemetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// The init function registers all metrics in this package to the prometheus registry
// exposed by the kubernetes controller manager
func init() {
	kubemetrics.Registry.MustRegister(netboxTotalRequests)
}

var (
	netboxTotalRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "netbox_requests_total",
		Help: "Total number of requests sent to the NetBox API server",
	},
		[]string{"status"},
	)
)

// IncrementNetboxRequests increments the netbox_total_requests metric with success/failure labels
func IncrementNetboxRequests(isSuccess bool) {
	if isSuccess {
		netboxTotalRequests.WithLabelValues("success").Inc()
	} else {
		netboxTotalRequests.WithLabelValues("failure").Inc()
	}
}
