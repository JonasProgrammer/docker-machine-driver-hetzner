//go:build !instrumented

package driver

const runningInstrumented = false

func instrumented[T any](input T) T {
	return input
}
