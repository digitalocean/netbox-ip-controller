FROM debian:bullseye

RUN apt-get update && apt-get install -y ca-certificates

ADD netbox-ip-controller /bin/netbox-ip-controller

ENTRYPOINT ["/bin/netbox-ip-controller"]
