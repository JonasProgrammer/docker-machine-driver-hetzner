//go:build !instrumented

package driver

import (
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const runningInstrumented = false

func instrumented[T any](input T) T {
	return input
}

func (d *Driver) setupClientInstrumentation(opts []hcloud.ClientOption) []hcloud.ClientOption {
	return opts
}
