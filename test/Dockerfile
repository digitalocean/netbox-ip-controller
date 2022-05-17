ARG GO_VERSION
FROM golang:${GO_VERSION}-bullseye

ARG K8S_VERSION
ARG ETCD_VERSION

RUN apt-get update && \
    apt-get -qqy install \
    ca-certificates \
    apt-transport-https \
    unzip \
    make \
    wget && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

RUN wget -qO /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v${K8S_VERSION}/bin/linux/amd64/kubectl && \
    chmod +x /usr/local/bin/kubectl

RUN cd /var/tmp && \
    mkdir -p /usr/local/kubebuilder/bin && \
		wget -q https://github.com/etcd-io/etcd/releases/download/v${ETCD_VERSION}/etcd-v${ETCD_VERSION}-linux-amd64.tar.gz && \
    tar -zxf etcd-v${ETCD_VERSION}-linux-amd64.tar.gz && \
		cp etcd-v${ETCD_VERSION}-linux-amd64/etcd /usr/local/kubebuilder/bin && \
		rm -rf /var/tmp/*

RUN cd /var/tmp && \
    mkdir -p /usr/local/kubebuilder/bin && \
		wget -q https://dl.k8s.io/v${K8S_VERSION}/kubernetes-server-linux-amd64.tar.gz && \
    tar -zxf kubernetes-server-linux-amd64.tar.gz && \
		cp kubernetes/server/bin/kube-apiserver /usr/local/kubebuilder/bin

WORKDIR /