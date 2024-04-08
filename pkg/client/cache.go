package client

import (
	"github.com/nutanix-cloud-native/prism-go-client/v3"
)

// NutanixClientCache is the cache of prism clients to be shared across the different controllers
var NutanixClientCache = v3.NewClientCache()
