package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	"github.com/jonasprogrammer/docker-machine-driver-hetzner/driver"
)

func main() {
	plugin.RegisterDriver(driver.NewDriver())
}
