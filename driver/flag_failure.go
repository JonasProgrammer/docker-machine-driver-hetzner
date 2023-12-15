//go:build !flag_debug && !instrumented

package driver

import (
	"fmt"

	"github.com/docker/machine/libmachine/drivers"
)

func (d *Driver) flagFailure(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

func (d *Driver) setConfigFromFlags(opts drivers.DriverOptions) error {
	return d.setConfigFromFlagsImpl(opts)
}
