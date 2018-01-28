package main

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
	"golang.org/x/crypto/ssh"

	"context"
	"github.com/hetznercloud/hcloud-go/hcloud"
)

type Driver struct {
	*drivers.BaseDriver

	AccessToken    string
	Image          string
	cachedImage    *hcloud.Image
	Type           string
	cachedType     *hcloud.ServerType
	Location       string
	cachedLocation *hcloud.Location
	KeyID          int
	cachedKey      *hcloud.SSHKey
	IsExistingKey  bool
	originalKey    string
	danglingKey    bool
	ServerID       int
	cachedServer   *hcloud.Server
}

const (
	defaultImage = "debian-9"
	defaultType  = "cx11"

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
		danglingKey:   false,
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
		return fmt.Errorf("hetzner requires --%v to be set", flagApiToken)
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	if d.IsExistingKey {
		if d.originalKey == "" {
			return fmt.Errorf("specifing an existing key ID requires the existing key path to be set as well")
		}

		key, err := d.getKey()

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
			return fmt.Errorf("remote key %d does not match local key %s", d.KeyID, d.originalKey)
		}
	}

	if _, err := d.getType(); err != nil {
		return err
	}

	if _, err := d.getImage(); err != nil {
		return err
	}

	if _, err := d.getLocation(); err != nil {
		return err
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

		keyopts := hcloud.SSHKeyCreateOpts{
			Name:      d.GetMachineName(),
			PublicKey: string(buf),
		}

		key, _, err := d.getClient().SSHKey.Create(context.Background(), keyopts)

		if err != nil {
			return err
		}

		d.KeyID = key.ID
		d.danglingKey = true

		defer d.destroyDanglingKey()
	}

	log.Infof("Creating Hetzner server...")

	srvopts := hcloud.ServerCreateOpts{Name: d.GetMachineName()}

	var err error
	if srvopts.Location, err = d.getLocation(); err != nil {
		return err
	}
	if srvopts.ServerType, err = d.getType(); err != nil {
		return err
	}
	if srvopts.Image, err = d.getImage(); err != nil {
		return err
	}
	if key, err := d.getKey(); err == nil {
		srvopts.SSHKeys = append(srvopts.SSHKeys, key)
	} else {
		return err
	}

	srv, _, err := d.getClient().Server.Create(context.Background(), srvopts)

	if err != nil {
		return err
	}

	log.Infof(" -> Creating server %s[%d] in %s[%d]", srv.Server.Name, srv.Server.ID, srv.Action.Command, srv.Action.ID)

	if err = d.waitForAction(srv.Action); err != nil {
		return err
	}

	d.ServerID = srv.Server.ID
	log.Infof(" -> Server %s[%d]: Waiting to come up...", srv.Server.Name, srv.Server.ID)

	for {
		srvstate, err := d.GetState()

		if err != nil {
			return err
		}

		if srvstate == state.Running {
			break
		}

		time.Sleep(1 * time.Second)
	}

	log.Debugf(" -> Server %s[%d] ready", srv.Server.Name, srv.Server.ID)
	d.IPAddress = srv.Server.PublicNet.IPv4.IP.String()

	d.danglingKey = false

	return nil
}

func (d *Driver) destroyDanglingKey() {
	if d.danglingKey && !d.IsExistingKey && d.KeyID != 0 {
		key, err := d.getKey()

		if err != nil {
			log.Error(err)
			return
		}

		if _, err := d.getClient().SSHKey.Delete(context.Background(), key); err != nil {
			log.Error(err)
			return
		}

		d.KeyID = 0
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
	srv, _, err := d.getClient().Server.GetByID(context.Background(), d.ServerID)

	if err != nil {
		return state.None, err
	}

	switch srv.Status {
	case hcloud.ServerStatusInitializing:
		return state.Starting, nil
	case hcloud.ServerStatusRunning:
		return state.Running, nil
	case hcloud.ServerStatusOff:
		return state.Stopped, nil
	}
	return state.None, nil
}

func (d *Driver) Remove() error {
	if d.ServerID != 0 {
		srv, err := d.getServerHandle()

		if err != nil {
			return err
		}

		log.Infof(" -> Destroying server %s[%d] in...", srv.Name, srv.ID)

		if _, err := d.getClient().Server.Delete(context.Background(), srv); err != nil {
			return err
		}
	}

	if !d.IsExistingKey && d.KeyID != 0 {
		key, err := d.getKey()

		if err != nil {
			return err
		}

		log.Infof(" -> Destroying SSHKey %s[%d]...", key.Name, key.ID)

		if _, err := d.getClient().SSHKey.Delete(context.Background(), key); err != nil {
			return err
		}
	}

	return nil
}

func (d *Driver) Restart() error {
	srv, err := d.getServerHandle()

	act, _, err := d.getClient().Server.Reboot(context.Background(), srv)
	if err != nil {
		return err
	}

	log.Infof(" -> Rebooting server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Start() error {
	srv, err := d.getServerHandle()

	act, _, err := d.getClient().Server.Poweron(context.Background(), srv)
	if err != nil {
		return err
	}

	log.Infof(" -> Starting server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Stop() error {
	srv, err := d.getServerHandle()

	act, _, err := d.getClient().Server.Shutdown(context.Background(), srv)
	if err != nil {
		return err
	}

	log.Infof(" -> Shutting down server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Kill() error {
	srv, err := d.getServerHandle()

	act, _, err := d.getClient().Server.Poweroff(context.Background(), srv)
	if err != nil {
		return err
	}

	log.Infof(" -> Powering off server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) getClient() *hcloud.Client {
	return hcloud.NewClient(hcloud.WithToken(d.AccessToken))
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

func (d *Driver) getLocation() (*hcloud.Location, error) {
	if d.cachedLocation != nil {
		return d.cachedLocation, nil
	}

	location, _, err := d.getClient().Location.GetByName(context.Background(), d.Location)

	if err == nil {
		d.cachedLocation = location
	}

	return location, err
}

func (d *Driver) getType() (*hcloud.ServerType, error) {
	if d.cachedType != nil {
		return d.cachedType, nil
	}

	stype, _, err := d.getClient().ServerType.GetByName(context.Background(), d.Type)

	if err == nil {
		d.cachedType = stype
	}

	return stype, err
}

func (d *Driver) getImage() (*hcloud.Image, error) {
	if d.cachedImage != nil {
		return d.cachedImage, nil
	}

	image, _, err := d.getClient().Image.GetByName(context.Background(), d.Image)

	if err == nil {
		d.cachedImage = image
	}

	return image, err
}

func (d *Driver) getKey() (*hcloud.SSHKey, error) {
	if d.cachedKey != nil {
		return d.cachedKey, nil
	}

	stype, _, err := d.getClient().SSHKey.GetByID(context.Background(), d.KeyID)

	if err == nil {
		d.cachedKey = stype
	}

	return stype, err
}

func (d *Driver) getServerHandle() (*hcloud.Server, error) {
	if d.cachedServer != nil {
		return d.cachedServer, nil
	}

	if d.ServerID == 0 {
		return nil, fmt.Errorf("server ID was 0")
	}

	srv, _, err := d.getClient().Server.GetByID(context.Background(), d.ServerID)

	if err != nil {
		d.cachedServer = srv
	}

	return srv, err
}

func (d *Driver) waitForAction(a *hcloud.Action) error {
	for {
		act, _, err := d.getClient().Action.GetByID(context.Background(), a.ID)

		if err != nil {
			return err
		}

		if act.Status == hcloud.ActionStatusSuccess {
			log.Debugf(" -> finished %s[%d]", act.Command, act.ID)
			break
		} else if act.Status == hcloud.ActionStatusRunning {
			log.Debugf(" -> %s[%d]: %d %%", act.Command, act.ID, act.Progress)
		} else if act.Status == hcloud.ActionStatusError {
			return act.Error()
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}
