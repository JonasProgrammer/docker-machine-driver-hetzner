package driver

import (
	"fmt"
	"os"

	"io/ioutil"

	"net"

	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/mcnutils"
	mcnssh "github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
	"github.com/jonasprogrammer/docker-machine-driver-hetzner/driver/hetzner"
	"golang.org/x/crypto/ssh"
)

type Driver struct {
	*drivers.BaseDriver

	AccessToken   string
	Image         string
	Type          string
	Location      string
	KeyID         int
	IsExistingKey bool
	originalKey   string
	ServerID      int
}

const (
	defaultImage = "debian-9"
	defaultType  = "g2-local"

	flagApiToken  = "hetzner-api-token"
	flagImage     = "hetzner-image"
	flagType      = "hetzner-server-type"
	flagLocation  = "hetzner-server-location"
	flagExKeyId   = "hetzner-existing-key-id"
	flagExKeyPath = "hetzner-existing-key-path"
)

func NewDriver() *Driver {
	return &Driver{
		Image:         defaultImage,
		Type:          defaultType,
		IsExistingKey: false,
		BaseDriver: &drivers.BaseDriver{
			SSHUser: drivers.DefaultSSHUser,
			SSHPort: drivers.DefaultSSHPort,
		},
	}
}

func (d *Driver) DriverName() string {
	return "hetzner"
}

func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "HETZNER_API_TOKEN",
			Name:   flagApiToken,
			Usage:  "Project-specific Hetzner API token",
			Value:  "",
		},

		mcnflag.StringFlag{
			EnvVar: "HETZNER_IMAGE",
			Name:   flagImage,
			Usage:  "Image to use for server creation",
			Value:  defaultImage,
		},

		mcnflag.StringFlag{
			EnvVar: "HETZNER_TYPE",
			Name:   flagType,
			Usage:  "Server type to create",
			Value:  defaultType,
		},

		mcnflag.StringFlag{
			EnvVar: "HETZNER_LOCATION",
			Name:   flagLocation,
			Usage:  "Location to create machine at",
			Value:  "",
		},

		mcnflag.IntFlag{
			EnvVar: "HETZNER_EXISTING_KEY_ID",
			Name:   flagExKeyId,
			Usage:  "Existing key ID to use for server; requires --hetzner-existing-key-path",
			Value:  0,
		},

		mcnflag.StringFlag{
			EnvVar: "HETZNER_EXISTING_KEY_PATH",
			Name:   flagExKeyPath,
			Usage:  "Path to existing key (new public key will be created unless --hetzner-existing-key-id is specified)",
			Value:  "",
		},
	}
}

func (d *Driver) SetConfigFromFlags(opts drivers.DriverOptions) error {
	d.AccessToken = opts.String(flagApiToken)
	d.Image = opts.String(flagImage)
	d.Location = opts.String(flagLocation)
	d.Type = opts.String(flagType)
	d.KeyID = opts.Int(flagExKeyId)
	d.IsExistingKey = d.KeyID != 0
	d.originalKey = opts.String(flagExKeyPath)

	d.SetSwarmConfigFromFlags(opts)

	if d.AccessToken == "" {
		return fmt.Errorf("hetnzer erquires --%v to be set", flagApiToken)
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	// TODO: Validate location, type and image to exist

	if d.IsExistingKey {
		if d.originalKey == "" {
			return fmt.Errorf("specifing an existing key ID requires the existing key path to be set as well")
		}

		key, err := d.getClient().GetSSHKey(d.KeyID)

		if err != nil {
			return err
		}

		buf, err := ioutil.ReadFile(d.originalKey + ".pub")
		if err != nil {
			return err
		}

		// Will also parse `ssh-rsa w309jwf0e39jf asdf` public keys
		pubk, _, _, _, err := ssh.ParseAuthorizedKey(buf)
		if err != nil {
			return err
		}

		if key.Fingerprint != ssh.FingerprintLegacyMD5(pubk) &&
			key.Fingerprint != ssh.FingerprintSHA256(pubk) {
			return fmt.Errorf("remote key %d does not fit with local key %s", d.KeyID, d.originalKey)
		}
	}

	return nil
}

func (d *Driver) Create() error {
	if d.originalKey != "" {
		log.Debugf("Copying SSH key...")
		if err := d.copySSHKeyPair(d.originalKey); err != nil {
			return err
		}
	} else {
		log.Debugf("Generating SSH key...")
		if err := mcnssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
			return err
		}
	}

	if d.KeyID == 0 {
		log.Infof("Creating SSH key...")

		buf, err := ioutil.ReadFile(d.GetSSHKeyPath() + ".pub")
		if err != nil {
			return err
		}

		key, err := d.getClient().CreateSSHKey(d.GetMachineName(), string(buf))
		if err != nil {
			return err
		}

		d.KeyID = key.Id
	}

	log.Infof("Creating Hetzner server...")

	srv, act, err := d.getClient().CreateServer(d.GetMachineName(), d.Type, d.Image, d.Location, d.KeyID)

	if err != nil {
		d.destroyDanglingKey()
		return err
	}

	log.Infof(" -> Creating server %s[%d] in %s[%d]", srv.Name, srv.Id, act.Command, act.Id)

	if err = d.waitForAction(act); err != nil {
		d.destroyDanglingKey()
		return err
	}

	d.ServerID = srv.Id
	log.Infof(" -> Server %s[%d]: Waiting to come up...", srv.Name, srv.Id)

	for {
		srvstate, err := d.GetState()

		if err != nil {
			d.destroyDanglingKey()
			return err
		}

		if srvstate == state.Running {
			break
		}

		time.Sleep(1 * time.Second)
	}

	log.Debugf(" -> Server %s[%d] ready", srv.Name, srv.Id)
	d.IPAddress = srv.PublicNet.IPv4.IP

	return nil
}

func (d *Driver) destroyDanglingKey() {
	if !d.IsExistingKey && d.KeyID != 0 {
		d.getClient().DeleteSSHKey(d.KeyID)
	}
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

func (d *Driver) GetURL() (string, error) {
	if err := drivers.MustBeRunning(d); err != nil {
		return "", err
	}

	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("tcp://%s", net.JoinHostPort(ip, "2376")), nil
}

func (d *Driver) GetState() (state.State, error) {
	srv, err := d.getClient().GetServer(d.ServerID)

	if err != nil {
		return state.None, err
	}

	switch srv.Status {
	case "initializing":
	case "starting":
		return state.Starting, nil
	case "running":
		return state.Running, nil
	case "stopping":
		return state.Stopping, nil
	case "off":
		return state.Stopped, nil
	}
	return state.None, nil
}

func (d *Driver) Remove() error {
	if d.ServerID != 0 {
		act, err := d.getClient().DeleteServer(d.ServerID)

		if err != nil {
			return err
		}

		log.Infof(" -> Destroying server %d in %s[%d]...", d.ServerID, act.Command, act.Id)

		if err = d.waitForAction(act); err != nil {
			return err
		}
	}

	if !d.IsExistingKey {
		log.Infof(" -> Destroying SSHKey %d...", d.KeyID)
		if err := d.getClient().DeleteSSHKey(d.KeyID); err != nil {
			return err
		}
	}

	return nil
}

func (d *Driver) Restart() error {
	act, err := d.getClient().RebootServer(d.ServerID)
	if err != nil {
		return err
	}

	log.Infof(" -> Rebooting server %d in %s[%d]...", d.ServerID, act.Command, act.Id)

	return d.waitForAction(act)
}

func (d *Driver) Start() error {
	act, err := d.getClient().PowerOnServer(d.ServerID)
	if err != nil {
		return err
	}

	log.Infof(" -> Starting server %d in %s[%d]...", d.ServerID, act.Command, act.Id)

	return d.waitForAction(act)
}

func (d *Driver) Stop() error {
	act, err := d.getClient().ShutdownServer(d.ServerID)
	if err != nil {
		return err
	}

	log.Infof(" -> Shutting down server %d in %s[%d]...", d.ServerID, act.Command, act.Id)

	return d.waitForAction(act)
}

func (d *Driver) Kill() error {
	act, err := d.getClient().PowerOffServer(d.ServerID)
	if err != nil {
		return err
	}

	log.Infof(" -> Powering off server %d in %s[%d]...", d.ServerID, act.Command, act.Id)

	return d.waitForAction(act)
}

func (d *Driver) getClient() *hetzner.Client {
	return hetzner.NewClient(d.AccessToken)
}

func (d *Driver) copySSHKeyPair(src string) error {
	if err := mcnutils.CopyFile(src, d.GetSSHKeyPath()); err != nil {
		return fmt.Errorf("unable to copy ssh key: %s", err)
	}

	if err := mcnutils.CopyFile(src+".pub", d.GetSSHKeyPath()+".pub"); err != nil {
		return fmt.Errorf("unable to copy ssh public key: %s", err)
	}

	if err := os.Chmod(d.GetSSHKeyPath(), 0600); err != nil {
		return fmt.Errorf("unable to set permissions on the ssh key: %s", err)
	}

	return nil
}

func (d *Driver) waitForAction(a *hetzner.Action) error {
	for {
		act, err := d.getClient().GetAction(a.Id)

		if err != nil {
			return err
		}

		if act.Status == "success" {
			log.Debugf(" -> finished %s[%d]", act.Command, act.Id)
			break
		} else if act.Status == "running" {
			log.Debugf(" -> %s[%d]: %d %%", act.Command, act.Id, act.Progress)
		} else if act.Status == "error" {
			if act.Error != nil {
				return fmt.Errorf("%s[%d] %s: %s", act.Command, act.Id, act.Error.Code, act.Error.Message)
			} else {
				return fmt.Errorf("%s[%d]: failed for unknown reason", act.Command, act.Id)
			}
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}
