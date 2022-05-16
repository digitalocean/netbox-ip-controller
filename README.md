# NetBox IP Controller

This controller watches Kubernetes pods and services and imports their IPs into NetBox.

Under development.

## Configuration

Controller configuration may be specified with either flags or environment variables, with
flags taking precedence.
For each of the flags listed below, the corresponding environment variable is all-uppercase
with dashes (`-`) replaced with underscores (`_`).

 Flag | Default | Description
------|---------|------------
`netbox-api-url` | | The URL of the NetBox API to connect to: `scheme://host:port/path`. Required.
`netbox-token` | | NetBox API token to use for authentication. Required.
`kube-config` | | Path to the kubeconfig file containing the address of the kube-apiserver to connect to and authentication info. The cluster you want the controller to connect to should be set as current context in the kubeconfig. Leave empty if the controller is running in-cluster. Optional.
`kube-qps` | `20` | Maximum number of requests per second to the kube-apiserver. Optional.
`kube-burst` | `30` | Maximum number of requests to the kube-apiserver allowed to accumulate before throttling begins. Optional.
`netbox-qps` | `100` | Average allowable requests per second to NetBox API, i.e., the rate limiter's token bucket refill rate per second
`netbox-burst` | `1` | Maximum allowable burst of requests to NetBox API, i.e. the rate limiter's token bucket size
`metrics-addr` | `:8001` | Sets the address to serve metrics on. Optional.
`cluster-domain` | `cluster.local` | Domain name of the cluster. Optional.
`pod-ip-tags` | `kubernetes,k8s-pod` | Comma-separated list of tags to add to pod IPs in NetBox. Any tags that don't yet exist will be created. Optional.
`service-ip-tags` | `kubernetes,k8s-service` | Comma-separated list of tags to add to service IPs in NetBox. Any tags that don't yet exist will be created. Optional.
`pod-publish-labels` | `app` | Comma-separated list of kubernetes pod labels to be added to the IP description in NetBox in `label: label_value` format. Optional. 
`service-publish-labels` | `app` | Comma-separated list of kubernetes service labels to be added to the IP description in NetBox in `label: label_value` format. Optional. 
`debug` | `false` | Turns on debug logging. Optional.

## Uninstall

After stopping netbox-ip-controller, the IP addresses published to NetBox by the controller will remain.
You can perform cleanup by running `netbox-ip-controller clean`, which will delete the IPs from NetBox
and remove `NetBoxIP` custom resource objects from the cluster.
Make sure to supply the same `netbox-api-url`, `netbox-token`, and `kube-config` (if any) as those used
by the running controller.
