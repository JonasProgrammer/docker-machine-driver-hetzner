//go:build instrumented

package driver

import (
	"encoding/json"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"os"
	"runtime/debug"

	"github.com/docker/machine/libmachine/log"
)

const runningInstrumented = false

func instrumented[T any](input T) T {
	j, err := json.Marshal(input)
	if err != nil {
		log.Error(err)
		panic(err)
	}
	log.Debugf("%v\n%v\n", string(debug.Stack()), string(j))
	return input
}

type debugLogWriter struct {
}

func (x debugLogWriter) Write(data []byte) (int, error) {
	log.Debug(string(data))
	return len(data), nil
}

func (d *Driver) setupClientInstrumentation(opts []hcloud.ClientOption) []hcloud.ClientOption {
	if os.Getenv("HETZNER_DRIVER_HTTP_DEBUG") == "42" {
		opts = append(opts, hcloud.WithDebugWriter(debugLogWriter{}))
	}
	return opts
}
