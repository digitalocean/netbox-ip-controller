// package metrics contains all custom metrics to be exported to prometheus
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	kubemetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Init registers all internal metrics to the given prometheus registry
func RegisterCustomMetrics() {
	kubemetrics.Registry.MustRegister(netboxTotalRequests)
}

var (
	netboxTotalRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "netbox_requests_total",
		Help: "Total number of requests sent to the NetBox API server",
	})
)

// IncremementNetboxTotalRequests increments the netbox_total_requests metric
func IncrementNetboxTotalRequests() {
	netboxTotalRequests.Inc()
}
