//go:build !instrumented

package main

func instrumented[T any](input T) T {
	return input
}
