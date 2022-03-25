package netboxipcontroller

// IPFinalizer is the finalizer that blocks object deletion
// until netbox-ip-controller removes object's IP from NetBox.
const IPFinalizer = "netbox.digitalocean.com/netbox-ip-controller"
