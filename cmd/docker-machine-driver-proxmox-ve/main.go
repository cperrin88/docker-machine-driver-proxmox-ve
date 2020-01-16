package main

import (
	proxmoxve "github.com/cperrin88/docker-machine-driver-proxmox-ve"
	"github.com/docker/machine/libmachine/drivers/plugin"
)

func main() {
	plugin.RegisterDriver(proxmoxve.NewDriver("default", ""))
}
