package driver

import (
	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/mcnflag"
)

type Driver struct {
	*drivers.BaseDriver

	SSHKeyPair *ssh.KeyPair
}

func NewDriver() *Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser: drivers.DefaultSSHUser,
			SSHPort: drivers.DefaultSSHPort,
		},
	}
}

func (d *Driver) DriverName() string {
	return "hetzner"
}

const (
	defaultImage    = "debian-9"
	defaultType     = "g2-local"
	defaultLocation = "fsn1"
)

func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "HETZNER_API_TOKEN",
			Name:   "hetzner-api-token",
			Usage:  "Your project-specific Hetzner API token",
			Value:  "",
		},
	}
}
