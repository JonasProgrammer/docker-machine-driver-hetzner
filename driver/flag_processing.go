package driver

import (
	"fmt"
	"strings"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

var legacyDefaultImages = [...]string{
	defaultImage,
	"ubuntu-18.04",
	"ubuntu-16.04",
	"debian-9",
}

func isDefaultImageName(imageName string) bool {
	for _, defaultImage := range legacyDefaultImages {
		if imageName == defaultImage {
			return true
		}
	}
	return false
}

func (d *Driver) setImageArch(arch string) error {
	switch arch {
	case "":
		d.ImageArch = emptyImageArchitecture
	case string(hcloud.ArchitectureARM):
		d.ImageArch = hcloud.ArchitectureARM
	case string(hcloud.ArchitectureX86):
		d.ImageArch = hcloud.ArchitectureX86
	default:
		return fmt.Errorf("unknown architecture %v", arch)
	}
	return nil
}

func (d *Driver) verifyImageFlags() error {
	if d.ImageID != 0 && d.Image != "" && !isDefaultImageName(d.Image) /* support legacy behaviour */ {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagImage, flagImageID)
	} else if d.ImageID != 0 && d.ImageArch != "" {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagImageArch, flagImageID)
	} else if d.ImageID == 0 && d.Image == "" {
		d.Image = defaultImage
	}
	return nil
}

func (d *Driver) verifyNetworkFlags() error {
	if d.DisablePublic4 && d.DisablePublic6 && !d.UsePrivateNetwork {
		return d.flagFailure("--%v must be used if public networking is disabled (hint: implicitly set by --%v)",
			flagUsePrivateNetwork, flagDisablePublic)
	}

	if d.DisablePublic4 && d.PrimaryIPv4 != "" {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagPrimary4, flagDisablePublic4)
	}

	if d.DisablePublic6 && d.PrimaryIPv6 != "" {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagPrimary6, flagDisablePublic6)
	}
	return nil
}

func (d *Driver) deprecatedBooleanFlag(opts drivers.DriverOptions, flag, deprecatedFlag string) bool {
	if opts.Bool(deprecatedFlag) {
		log.Warnf("--%v is DEPRECATED FOR REMOVAL, use --%v instead", deprecatedFlag, flag)
		d.usesDfr = true
		return true
	}
	return opts.Bool(flag)
}

func (d *Driver) setUserDataFlags(opts drivers.DriverOptions) error {
	userData := opts.String(flagUserData)
	userDataFile := opts.String(flagUserDataFile)

	if opts.Bool(legacyFlagUserDataFromFile) {
		if userDataFile != "" {
			return d.flagFailure("--%v and --%v are mutually exclusive", flagUserDataFile, legacyFlagUserDataFromFile)
		}

		log.Warnf("--%v is DEPRECATED FOR REMOVAL, pass '--%v \"%v\"'", legacyFlagUserDataFromFile, flagUserDataFile, userData)
		d.usesDfr = true
		d.userDataFile = userData
		return nil
	}

	d.userData = userData
	d.userDataFile = userDataFile

	if d.userData != "" && d.userDataFile != "" {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagUserData, flagUserDataFile)
	}

	return nil
}

func (d *Driver) setLabelsFromFlags(opts drivers.DriverOptions) error {
	d.ServerLabels = make(map[string]string)
	for _, label := range opts.StringSlice(flagServerLabel) {
		split := strings.SplitN(label, "=", 2)
		if len(split) != 2 {
			return d.flagFailure("server label %v is not in key=value format", label)
		}
		d.ServerLabels[split[0]] = split[1]
	}
	d.keyLabels = make(map[string]string)
	for _, label := range opts.StringSlice(flagKeyLabel) {
		split := strings.SplitN(label, "=", 2)
		if len(split) != 2 {
			return fmt.Errorf("key label %v is not in key=value format", label)
		}
		d.keyLabels[split[0]] = split[1]
	}
	return nil
}
