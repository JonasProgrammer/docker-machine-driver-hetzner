package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/docker/machine/libmachine/drivers/plugin"
)

// Version will be added once we start the build process via travis-ci
var Version string

func main() {
	version := flag.Bool("v", false, "prints current docker-machine-driver-hetzner version")
	flag.Parse()
	if *version {
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}
	plugin.RegisterDriver(NewDriver())
}
