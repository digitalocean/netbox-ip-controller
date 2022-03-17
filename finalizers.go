package netboxipcontroller

// IPFinalizer is the finalizer that blocks object deletion
// until netbox-ip-controller removes object's IP from NetBox.
const IPFinalizer = "digitalocean.com/netbox-ip-controller"
