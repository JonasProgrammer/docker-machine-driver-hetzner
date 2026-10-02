package driver

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/machine/commands/commandstest"
	"github.com/docker/machine/libmachine/drivers"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
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
	d := NewDriver("test")
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagUserData:     inlineContents,
		flagUserDataFile: file,
	}))
	assertMutualExclusion(t, err, flagUserData, flagUserDataFile)

	// mutual exclusion data file <=> legacy flag
	d = NewDriver("test")
	err = d.setConfigFromFlagsImpl(&commandstest.FakeFlagger{
		Data: map[string]interface{}{
			legacyFlagUserDataFromFile: true,
			flagUserDataFile:           file,
		},
	})
	assertMutualExclusion(t, err, legacyFlagUserDataFromFile, flagUserDataFile)

	// inline user data
	d = NewDriver("test")
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
	d = NewDriver("test")
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
	d = NewDriver("test")
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
	d := NewDriver("test")
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
	d := NewDriver("test")
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
	d = NewDriver("test")
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
	d := NewDriver("test")
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
	d = NewDriver("test")
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

func TestImageFlagExclusions(t *testing.T) {
	// both id and name given
	d := NewDriver("test")
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImageID: "42",
		flagImage:   "answer",
	}))
	assertMutualExclusion(t, err, flagImageID, flagImage)

	// both id and arch given
	d = NewDriver("test")
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImageID:   "42",
		flagImageArch: string(hcloud.ArchitectureX86),
	}))
	assertMutualExclusion(t, err, flagImageID, flagImageArch)
}

func TestImageArch(t *testing.T) {
	// no explicit arch
	d := NewDriver("test")
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImage: "answer",
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if d.ImageArch != emptyImageArchitecture {
		t.Errorf("expected empty architecture, but got %v", d.ImageArch)
	}

	// existing architectures
	testArchFlag(t, hcloud.ArchitectureARM)
	testArchFlag(t, hcloud.ArchitectureX86)

	// invalid
	d = NewDriver("test")
	err = d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImage:     "answer",
		flagImageArch: "hal9000",
	}))
	if err == nil {
		t.Fatal("expected error, but invalid arch was accepted")
	}
}

func TestBogusId(t *testing.T) {
	d := NewDriver("test")
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImageID: "answer",
	}))
	if err == nil {
		t.Fatal("expected error, but invalid arch was accepted")
	}
}

func TestLongId(t *testing.T) {
	var testId int64 = 79871865169581

	d := NewDriver("test")
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImageID: strconv.FormatInt(testId, 10),
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if d.ImageID != testId {
		t.Errorf("expected %v id, but got %v", testId, d.ImageArch)
	}
}

func testArchFlag(t *testing.T, arch hcloud.Architecture) {
	d := NewDriver("test")
	err := d.setConfigFromFlagsImpl(makeFlags(map[string]interface{}{
		flagImage:     "answer",
		flagImageArch: string(arch),
	}))
	if err != nil {
		t.Fatalf("unexpected error, %v", err)
	}

	if d.ImageArch != arch {
		t.Errorf("expected %v architecture, but got %v", arch, d.ImageArch)
	}
}

func assertMutualExclusion(t *testing.T, err error, flag1, flag2 string) {
	if err == nil {
		t.Errorf("expected mutually exclusive flags to fail, but no error was thrown: %v %v", flag1, flag2)
		return
	}

	errstr := err.Error()
	if !strings.Contains(errstr, flag1) || !strings.Contains(errstr, flag2) || !strings.Contains(errstr, "mutually exclusive") {
		t.Errorf("expected mutually exclusive flags to fail, but message differs: %v %v %v", flag1, flag2, errstr)
	}
}

func TestParseNetworkSpec(t *testing.T) {
	tests := []struct {
		raw      string
		idOrName string
		ipRange  string
	}{
		{raw: "prod-net", idOrName: "prod-net"},
		{raw: "12345", idOrName: "12345"},
		{raw: "prod-net:192.168.20.0/24", idOrName: "prod-net", ipRange: "192.168.20.0/24"},
		{raw: "12345:10.0.1.0/24", idOrName: "12345", ipRange: "10.0.1.0/24"},
		{raw: "prod-net:192.168.20.5/24", idOrName: "prod-net:192.168.20.5/24"},
		{raw: "team:prod", idOrName: "team:prod"},
		{raw: "prod-net:not-a-cidr", idOrName: "prod-net:not-a-cidr"},
		{raw: ":192.168.20.0/24", idOrName: ":192.168.20.0/24"},
		{raw: "team:prod:10.0.1.0/24", idOrName: "team:prod", ipRange: "10.0.1.0/24"},
		{raw: "prod-net:10.0.1.0", idOrName: "prod-net:10.0.1.0"},
	}

	for _, tt := range tests {
		spec := parseNetworkSpec(tt.raw)
		if spec.IDOrName != tt.idOrName {
			t.Errorf("%q: expected id or name %q, got %q", tt.raw, tt.idOrName, spec.IDOrName)
		}

		ipRange := ""
		if spec.IPRange != nil {
			ipRange = spec.IPRange.String()
		}
		if ipRange != tt.ipRange {
			t.Errorf("%q: expected ip range %q, got %q", tt.raw, tt.ipRange, ipRange)
		}
	}
}

func TestVerifySubnetExists(t *testing.T) {
	_, appSubnet, _ := net.ParseCIDR("192.168.20.0/24")
	_, dbSubnet, _ := net.ParseCIDR("192.168.30.0/24")
	_, otherSubnet, _ := net.ParseCIDR("10.0.1.0/24")
	_, narrowSubnet, _ := net.ParseCIDR("192.168.20.0/25")
	network := &hcloud.Network{
		Name: "prod-net",
		Subnets: []hcloud.NetworkSubnet{
			{Type: hcloud.NetworkSubnetTypeCloud},
			{Type: hcloud.NetworkSubnetTypeCloud, IPRange: otherSubnet},
			{Type: hcloud.NetworkSubnetTypeCloud, IPRange: appSubnet},
		},
	}

	if err := verifySubnetExists(network, appSubnet); err != nil {
		t.Errorf("unexpected error, %v", err)
	}

	if err := verifySubnetExists(network, narrowSubnet); err == nil {
		t.Error("expected error for subnet with different prefix length")
	}

	err := verifySubnetExists(network, dbSubnet)
	if err == nil {
		t.Fatal("expected error for missing subnet")
	}
	if err.Error() != "network 'prod-net' has no subnet 192.168.30.0/24" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRequiresTemporaryPublicIPv6(t *testing.T) {
	network := &hcloud.Network{ID: 1}
	tests := []struct {
		name     string
		opts     hcloud.ServerCreateOpts
		expected bool
	}{
		{name: "default public net", opts: hcloud.ServerCreateOpts{}, expected: false},
		{name: "public disabled with network", opts: hcloud.ServerCreateOpts{PublicNet: &hcloud.ServerCreatePublicNet{}, Networks: []*hcloud.Network{network}}, expected: false},
		{name: "public disabled without network", opts: hcloud.ServerCreateOpts{PublicNet: &hcloud.ServerCreatePublicNet{}}, expected: true},
		{name: "only ipv4 disabled", opts: hcloud.ServerCreateOpts{PublicNet: &hcloud.ServerCreatePublicNet{EnableIPv6: true}}, expected: false},
		{name: "only ipv6 disabled", opts: hcloud.ServerCreateOpts{PublicNet: &hcloud.ServerCreatePublicNet{EnableIPv4: true}}, expected: false},
	}

	for _, tt := range tests {
		if actual := requiresTemporaryPublicIPv6(&tt.opts); actual != tt.expected {
			t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, actual)
		}
	}
}
