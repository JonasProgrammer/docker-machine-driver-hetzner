//go:build !flag_debug && !instrumented

package driver

import (
	"github.com/docker/machine/libmachine/drivers"
	"github.com/pkg/errors"
)

func (d *Driver) flagFailure(format string, args ...interface{}) error {
	return errors.Errorf(format, args...)
}

func (d *Driver) setConfigFromFlags(opts drivers.DriverOptions) error {
	return d.setConfigFromFlagsImpl(opts)
}
