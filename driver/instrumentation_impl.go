//go:build instrumented

package driver

import (
	"encoding/json"
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
