package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	"github.com/jonasprogrammer/docker-machine-hetzner-plugin/driver"
)

func main() {
	plugin.RegisterDriver(driver.NewDriver())
}
