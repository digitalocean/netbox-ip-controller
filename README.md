# NetBox IP Controller

This controller watches Kubernetes pods and services and imports their IPs,
along with some metadata such as domain names and Kubernetes labels, into NetBox.

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
`metrics-addr` | `:8001` | Sets the address that the controller will bind to for serving metrics. Can be a full TCP address or only a port (e.g. `:8081`). Optional.
`cluster-domain` | `cluster.local` | Domain name of the cluster. Optional.
`pod-ip-tags` | `kubernetes,k8s-pod` | Comma-separated list of tags to add to pod IPs in NetBox. Any tags that don't yet exist will be created. Optional.
`service-ip-tags` | `kubernetes,k8s-service` | Comma-separated list of tags to add to service IPs in NetBox. Any tags that don't yet exist will be created. Optional.
`pod-publish-labels` | `app` | Comma-separated list of kubernetes pod labels to be added to the IP description in NetBox in `label: label_value` format. Only the IPs of the pods that have at least one of these labels set will be exported. Set to an empty list if you do not want pod IPs exported. Optional. 
`service-publish-labels` | `app` | Comma-separated list of kubernetes service labels to be added to the IP description in NetBox in `label: label_value` format. Only the IPs of the services that have at least one of these labels set will be exported. Set to an empty list if you do not want service IPs exported. Optional. 
`dual-stack-ip` | `false` | Enables registering both IPv4 and IPv6 addresses of pods and services where applicable in dual stack clusters. Optional.
`ready-check-addr` | `:5001` | Sets the address that the controller manager will bind to for serving the ready check endpoint. Can be a full TCP address or only a port (e.g. `:5001`). Optional. 
`debug` | `false` | Turns on debug logging. Optional.

## Running locally

The most basic setup includes a NetBox and Kubernetes apiserver to connect to. The controller will be using `current-context` from the specified kubeconfig:

```
go get github.com/digitalocean/netbox-ip-controller/cmd/netbox-ip-controller
netbox-ip-controller --kube-config=/.kube/config --netbox-api-url=https://some-netbox.example.com/api --netbox-token=<your-token> \
  
```

## Running integration tests locally

Integration tests can be run locally by using the `integration-test` make target.
This sets up, executes, and cleans up the integration test. Alternatively, you can use the `setup`, `execute`, and `cleanup`
targets individually, which can be helpful for leaving the netbox environment up after executing tests for debugging.

## Install

A sample deployment for running in-cluster can be found at [docs/example-deployment.yml](docs/example-deployment.yml).
**Note** that the controller will only export the IPs of the pods and services that have at least one of `--pod-publish-labels` or
`--service-publish-labels` respectively set.

If you have RBAC enabled in the cluster, you will also need [docs/rbac.yml](/docs/rbac.yml).

Docker images are automatically built and distributed for each release and can be found at `digitalocean/netbox-ip-controller:<tag>`.
Image tags will always correspond to a release's version number. 

Alternatively, you can build and host the image yourself. After cloning the repo, build and push the docker image:
```
docker build -t <username>/netbox-ip-controller:<tag> ./cmd/netbox-ip-controller/
docker push <username>/netbox-ip-controller:<tag>
```
and use `<username>/netbox-ip-controller:<tag>` in your deployment manifest. 

## Uninstall

After stopping netbox-ip-controller, the IP addresses published to NetBox by the controller will remain.
You can perform cleanup by running `netbox-ip-controller clean`, which will delete the IPs from NetBox
and remove `NetBoxIP` custom resource objects from the cluster.
Make sure to supply the same `netbox-api-url`, `netbox-token`, and `kube-config` (if any) as those used
by the running controller.

## Contributing

Contributions are welcome and appreciated. To help us review code and resolve issues faster,
please follow the [guidelines](CONTRIBUTING.md).

## License

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
