//go:build flag_debug || instrumented

package driver

import (
	"encoding/json"
	"fmt"

	"github.com/docker/machine/libmachine/drivers"
)

var lastOpts drivers.DriverOptions

func (d *Driver) flagFailure(format string, args ...interface{}) error {
	// machine driver may not flush logs received when getting an RPC error, so we have to resort to this terribleness
	line1 := fmt.Sprintf("Flag failure detected:\n -> last opts: %v\n -> driver state %v", lastOpts, d)
	var line2 string
	if out, err := json.MarshalIndent(d, "", "  "); err == nil {
		line2 = fmt.Sprintf(" -> driver json:\n%s", out)
	} else {
		line2 = fmt.Sprintf("could not encode driver json: %v", err)
	}

	combined := append([]interface{}{line1, line2}, args...)
	return fmt.Errorf("%s\n%s\n"+format, combined...)
}

func (d *Driver) setConfigFromFlags(opts drivers.DriverOptions) error {
	lastOpts = opts
	return d.setConfigFromFlagsImpl(opts)
}
