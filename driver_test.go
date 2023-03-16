package main

import (
	"github.com/docker/machine/commands/commandstest"
	"github.com/docker/machine/libmachine/drivers"
	"os"
	"strings"
	"testing"
)

var defaultFlags = map[string]interface{}{
	flagAPIToken: "foo",
}

func makeFlags(args map[string]interface{}) drivers.DriverOptions {
	combined := make(map[string]interface{}, len(defaultFlags)+len(args))
	for k, v := range defaultFlags {
		combined[k] = v
	}
	for k, v := range args {
		combined[k] = v
	}

	return &commandstest.FakeFlagger{Data: combined}
}

func TestUserData(t *testing.T) {
	const fileContents = "User data from file"
	const inlineContents = "User data"

	file := t.TempDir() + string(os.PathSeparator) + "userData"
	err := os.WriteFile(file, []byte(fileContents), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// mutual exclusion data <=> data file
	d := NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagUserData:     inlineContents,
		flagUserDataFile: file,
	}))
	assertMutualExclusion(t, err, flagUserData, flagUserDataFile)

	// mutual exclusion data file <=> legacy flag
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(&commandstest.FakeFlagger{
		Data: map[string]interface{}{
			legacyFlagUserDataFromFile: true,
			flagUserDataFile:           file,
		},
	})
	assertMutualExclusion(t, err, legacyFlagUserDataFromFile, flagUserDataFile)

	// inline user data
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagUserData: inlineContents,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	data, err := d.getUserData()
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}
	if data != inlineContents {
		t.Error("content did not match (inline)")
	}

	// file user data
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagUserDataFile: file,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	data, err = d.getUserData()
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}
	if data != fileContents {
		t.Error("content did not match (file)")
	}

	// legacy file user data
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagUserData:               file,
		legacyFlagUserDataFromFile: true,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	data, err = d.getUserData()
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}
	if data != fileContents {
		t.Error("content did not match (legacy-file)")
	}
}

func TestDisablePublic(t *testing.T) {
	d := NewDriver()
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagDisablePublic: true,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if !d.DisablePublic4 {
		t.Error("expected public ipv4 to be disabled")
	}
	if !d.DisablePublic6 {
		t.Error("expected public ipv6 to be disabled")
	}
	if !d.UsePrivateNetwork {
		t.Error("expected private network to be enabled")
	}
}

func TestDisablePublic46(t *testing.T) {
	d := NewDriver()
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagDisablePublic4: true,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if !d.DisablePublic4 {
		t.Error("expected public ipv4 to be disabled")
	}
	if d.DisablePublic6 {
		t.Error("public ipv6 disabled unexpectedly")
	}
	if d.UsePrivateNetwork {
		t.Error("network enabled unexpectedly")
	}

	// 6
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagDisablePublic6: true,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if d.DisablePublic4 {
		t.Error("public ipv4 disabled unexpectedly")
	}
	if !d.DisablePublic6 {
		t.Error("expected public ipv6 to be disabled")
	}
	if d.UsePrivateNetwork {
		t.Error("network enabled unexpectedly")
	}
}

func TestDisablePublic46Legacy(t *testing.T) {
	d := NewDriver()
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		legacyFlagDisablePublic4: true,
		// any truthy flag should take precedence
		flagDisablePublic4: false,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if !d.DisablePublic4 {
		t.Error("expected public ipv4 to be disabled")
	}
	if d.DisablePublic6 {
		t.Error("public ipv6 disabled unexpectedly")
	}
	if d.UsePrivateNetwork {
		t.Error("network enabled unexpectedly")
	}

	// 6
	d = NewDriver()
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		legacyFlagDisablePublic6: true,
		// any truthy flag should take precedence
		flagDisablePublic6: false,
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if d.DisablePublic4 {
		t.Error("public ipv4 disabled unexpectedly")
	}
	if !d.DisablePublic6 {
		t.Error("expected public ipv6 to be disabled")
	}
	if d.UsePrivateNetwork {
		t.Error("network enabled unexpectedly")
	}
}

func assertMutualExclusion(t *testing.T, err error, flag1, flag2 string) {
	if err == nil {
		t.Errorf("expected mutually exclusive flags to fail, but no error was thrown: %v %v", flag1, flag2)
		return
	}

	errstr := err.Error()
	if !(strings.Contains(errstr, flag1) && strings.Contains(errstr, flag2) && strings.Contains(errstr, "mutually exclusive")) {
		t.Errorf("expected mutually exclusive flags to fail, but message differs: %v %v %v", flag1, flag2, errstr)
	}
}
