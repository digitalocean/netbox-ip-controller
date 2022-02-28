# NetBox IP Controller

This controller watches Kubernetes pods and services and imports their IPs into NetBox.

Under development.

## Configuration

 Env | Default | Description
-----|---------|------------
`METRICS_ADDR` | `:8001` | Sets the address to serve metrics on. Optional.
`NETBOX_API_URL` | | The URL of the NetBox API to connect to: `scheme://host:port/path`. Required.
`NETBOX_TOKEN` | | NetBox API token to use for authentication. Required.
`KUBE_CONFIG` | | Path to the kubeconfig file containing the address of the kube-apiserver to connect to and authentication info. The cluster you want the controller to connect to should be set as current context in the kubeconfig. Leave empty if the controller is running in-cluster. Optional.
`KUBE_QPS` | `20` | Maximum number of requests per second to the kube-apiserver. Optional.
`KUBE_BURST` | `30` | Maximum number of requests to the kube-apiserver allowed to accumulate before throttling begins. Optional.
