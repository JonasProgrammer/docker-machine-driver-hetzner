package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/mcnutils"
	mcnssh "github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

type Driver struct {
	*drivers.BaseDriver

	AccessToken       string
	Image             string
	ImageID           int
	cachedImage       *hcloud.Image
	Type              string
	cachedType        *hcloud.ServerType
	Location          string
	cachedLocation    *hcloud.Location
	KeyID             int
	cachedKey         *hcloud.SSHKey
	IsExistingKey     bool
	originalKey       string
	danglingKey       bool
	ServerID          int
	userData          string
	volumes           []string
	networks          []string
	UsePrivateNetwork bool
	cachedServer      *hcloud.Server
	ForcePublicSSH    bool
	PublicIPAddress   string
}

const (
	defaultImage = "ubuntu-18.04"
	defaultType  = "cx11"

	flagAPIToken          = "hetzner-api-token"
	flagImage             = "hetzner-image"
	flagImageID           = "hetzner-image-id"
	flagType              = "hetzner-server-type"
	flagLocation          = "hetzner-server-location"
	flagExKeyID           = "hetzner-existing-key-id"
	flagExKeyPath         = "hetzner-existing-key-path"
	flagUserData          = "hetzner-user-data"
	flagVolumes           = "hetzner-volumes"
	flagNetworks          = "hetzner-networks"
	flagUsePrivateNetwork = "hetzner-use-private-network"
	flagForcePublicSSH    = "hetzner-force-public-ssh"
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
			Name:   flagAPIToken,
			Usage:  "Project-specific Hetzner API token",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_IMAGE",
			Name:   flagImage,
			Usage:  "Image to use for server creation",
			Value:  defaultImage,
		},
		mcnflag.IntFlag{
			EnvVar: "HETZNER_IMAGE_ID",
			Name:   flagImageID,
			Usage:  "Image to use for server creation",
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
			Name:   flagExKeyID,
			Usage:  "Existing key ID to use for server; requires --hetzner-existing-key-path",
			Value:  0,
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_EXISTING_KEY_PATH",
			Name:   flagExKeyPath,
			Usage:  "Path to existing key (new public key will be created unless --hetzner-existing-key-id is specified)",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_USER_DATA",
			Name:   flagUserData,
			Usage:  "Cloud-init based User data",
			Value:  "",
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_VOLUMES",
			Name:   flagVolumes,
			Usage:  "Volume IDs or names which should be attached to the server",
			Value:  []string{},
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_NETWORKS",
			Name:   flagNetworks,
			Usage:  "Network IDs or names which should be attached to the server private network interface",
			Value:  []string{},
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_USE_PRIVATE_NETWORK",
			Name:   flagUsePrivateNetwork,
			Usage:  "Use private network",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_FORCE_PUBLIC_SSH",
			Name:   flagForcePublicSSH,
			Usage:  "Force using the public IP address for SSH connections from the outside even when the private network is enabled",
		},
	}
}

func (d *Driver) SetConfigFromFlags(opts drivers.DriverOptions) error {
	d.AccessToken = opts.String(flagAPIToken)
	d.Image = opts.String(flagImage)
	d.ImageID = opts.Int(flagImageID)
	d.Location = opts.String(flagLocation)
	d.Type = opts.String(flagType)
	d.KeyID = opts.Int(flagExKeyID)
	d.IsExistingKey = d.KeyID != 0
	d.originalKey = opts.String(flagExKeyPath)
	d.userData = opts.String(flagUserData)
	d.volumes = opts.StringSlice(flagVolumes)
	d.networks = opts.StringSlice(flagNetworks)
	d.UsePrivateNetwork = opts.Bool(flagUsePrivateNetwork)
	d.ForcePublicSSH = opts.Bool(flagForcePublicSSH)

	d.SetSwarmConfigFromFlags(opts)

	if d.AccessToken == "" {
		return errors.Errorf("hetzner requires --%v to be set", flagAPIToken)
	}

	if d.ImageID != 0 && d.Image != defaultImage {
		return errors.Errorf("--%v and --%v are mutually exclusive", flagImage, flagImageID)
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	if d.IsExistingKey {
		if d.originalKey == "" {
			return errors.New("specifying an existing key ID requires the existing key path to be set as well")
		}

		key, err := d.getKey()
		if err != nil {
			return errors.Wrap(err, "could not get key")
		}

		buf, err := ioutil.ReadFile(d.originalKey + ".pub")
		if err != nil {
			return errors.Wrap(err, "could not read public key")
		}

		// Will also parse `ssh-rsa w309jwf0e39jf asdf` public keys
		pubk, _, _, _, err := ssh.ParseAuthorizedKey(buf)
		if err != nil {
			return errors.Wrap(err, "could not parse authorized key")
		}

		if key.Fingerprint != ssh.FingerprintLegacyMD5(pubk) &&
			key.Fingerprint != ssh.FingerprintSHA256(pubk) {
			return errors.Errorf("remote key %d does not match local key %s", d.KeyID, d.originalKey)
		}
	}

	if _, err := d.getType(); err != nil {
		return errors.Wrap(err, "could not get type")
	}

	if _, err := d.getImage(); err != nil {
		return errors.Wrap(err, "could not get image")
	}

	if _, err := d.getLocation(); err != nil {
		return errors.Wrap(err, "could not get location")
	}

	if d.UsePrivateNetwork && len(d.networks) == 0 {
		return errors.Errorf("No private network attached.")
	}

	return nil
}

func (d *Driver) Create() error {
	if d.originalKey != "" {
		log.Debugf("Copying SSH key...")
		if err := d.copySSHKeyPair(d.originalKey); err != nil {
			return errors.Wrap(err, "could not copy ssh key pair")
		}
	} else {
		log.Debugf("Generating SSH key...")
		if err := mcnssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
			return errors.Wrap(err, "could not generate ssh key")
		}
	}

	if d.KeyID == 0 {
		log.Infof("Creating SSH key...")

		buf, err := ioutil.ReadFile(d.GetSSHKeyPath() + ".pub")
		if err != nil {
			return errors.Wrap(err, "could not read ssh public key")
		}

		key, err := d.getRemoteKeyWithSameFingerprint(buf)
		if key == nil {
			log.Infof("SSH key not found in Hetzner. Uploading...")

			keyopts := hcloud.SSHKeyCreateOpts{
				Name:      d.GetMachineName(),
				PublicKey: string(buf),
			}

			key, _, err = d.getClient().SSHKey.Create(context.Background(), keyopts)
			if err != nil {
				return errors.Wrap(err, "could not create ssh key")
			} else if key == nil {
				return errors.Errorf("key upload did not return an error, but key was nil")
			}

			d.danglingKey = true
			defer d.destroyDanglingKey()
		} else {
			d.IsExistingKey = true
			log.Debugf("SSH key found in Hetzner. ID: %d", key.ID)
		}

		d.KeyID = key.ID
	}

	log.Infof("Creating Hetzner server...")

	srvopts := hcloud.ServerCreateOpts{
		Name:     d.GetMachineName(),
		UserData: d.userData,
	}
	networks := []*hcloud.Network{}
	for _, networkIDorName := range d.networks {
		network, _, err := d.getClient().Network.Get(context.Background(), networkIDorName)
		if err != nil {
			return errors.Wrap(err, "could not get network by ID or name")
		}
		if network == nil {
			return errors.Errorf("network '%s' not found", networkIDorName)
		}
		networks = append(networks, network)
	}
	srvopts.Networks = networks

	volumes := []*hcloud.Volume{}
	for _, volumeIDorName := range d.volumes {
		volume, _, err := d.getClient().Volume.Get(context.Background(), volumeIDorName)
		if err != nil {
			return errors.Wrap(err, "could not get volume by ID or name")
		}
		if volume == nil {
			return errors.Errorf("volume '%s' not found", volumeIDorName)
		}
		volumes = append(volumes, volume)
	}
	srvopts.Volumes = volumes

	var err error
	if srvopts.Location, err = d.getLocation(); err != nil {
		return errors.Wrap(err, "could not get location")
	}
	if srvopts.ServerType, err = d.getType(); err != nil {
		return errors.Wrap(err, "could not get type")
	}
	if srvopts.Image, err = d.getImage(); err != nil {
		return errors.Wrap(err, "could not get image")
	}
	key, err := d.getKey()
	if err != nil {
		return errors.Wrap(err, "could not get ssh key")
	}
	srvopts.SSHKeys = append(srvopts.SSHKeys, key)

	srv, _, err := d.getClient().Server.Create(context.Background(), srvopts)
	if err != nil {
		return errors.Wrap(err, "could not create server")
	}

	log.Infof(" -> Creating server %s[%d] in %s[%d]", srv.Server.Name, srv.Server.ID, srv.Action.Command, srv.Action.ID)
	if err = d.waitForAction(srv.Action); err != nil {
		return errors.Wrap(err, "could not wait for action")
	}

	d.ServerID = srv.Server.ID
	log.Infof(" -> Server %s[%d]: Waiting to come up...", srv.Server.Name, srv.Server.ID)

	for {
		srvstate, err := d.GetState()
		if err != nil {
			return errors.Wrap(err, "could not get state")
		}

		if srvstate == state.Running {
			break
		}

		time.Sleep(1 * time.Second)
	}

	if d.UsePrivateNetwork {
		for {
			// we need to wait until network is attached
			log.Infof("Wait until private network attached ...")
			server, _, err := d.getClient().Server.GetByID(context.Background(), srv.Server.ID)
			if err != nil {
				return errors.Wrapf(err, "could not get newly created server [%d]", srv.Server.ID)
			}
			if server.PrivateNet != nil {
				d.IPAddress = server.PrivateNet[0].IP.String()
				break
			}
			time.Sleep(1 * time.Second)
		}
	} else {
		log.Infof("Using public network ...")
		d.IPAddress = srv.Server.PublicNet.IPv4.IP.String()
	}

	d.PublicIPAddress = srv.Server.PublicNet.IPv4.IP.String()

	log.Infof(" -> Server %s[%d] ready. Ip %s", srv.Server.Name, srv.Server.ID, d.IPAddress)
	d.danglingKey = false

	return nil
}

func (d *Driver) destroyDanglingKey() {
	if d.danglingKey && !d.IsExistingKey && d.KeyID != 0 {
		key, err := d.getKey()
		if err != nil {
			log.Errorf("could not get key: %v", err)
			return
		}

		if _, err := d.getClient().SSHKey.Delete(context.Background(), key); err != nil {
			log.Errorf("could not delete ssh key: %v", err)
			return
		}
		d.KeyID = 0
	}
}

func (d *Driver) GetSSHHostname() (string, error) {
	if d.ForcePublicSSH {
		return d.PublicIPAddress, nil
	} else {
		return d.GetIP()
	}
}

func (d *Driver) GetURL() (string, error) {
	if err := drivers.MustBeRunning(d); err != nil {
		return "", errors.Wrap(err, "could not execute drivers.MustBeRunning")
	}

	ip, err := d.GetIP()
	if err != nil {
		return "", errors.Wrap(err, "could not get IP")
	}

	return fmt.Sprintf("tcp://%s", net.JoinHostPort(ip, "2376")), nil
}

func (d *Driver) GetState() (state.State, error) {
	srv, _, err := d.getClient().Server.GetByID(context.Background(), d.ServerID)
	if err != nil {
		return state.None, errors.Wrap(err, "could not get server by ID")
	}
	if srv == nil {
		return state.None, errors.New("server not found")
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
			return errors.Wrap(err, "could not get server handle")
		}

		if srv == nil {
			log.Infof(" -> Server does not exist anymore")
		} else {
			log.Infof(" -> Destroying server %s[%d] in...", srv.Name, srv.ID)

			if _, err := d.getClient().Server.Delete(context.Background(), srv); err != nil {
				return errors.Wrap(err, "could not delete server")
			}
		}
	}

	if !d.IsExistingKey && d.KeyID != 0 {
		key, err := d.getKey()
		if err != nil {
			return errors.Wrap(err, "could not get ssh key")
		}
		if key == nil {
			log.Infof(" -> SSH key does not exist anymore")
			return nil
		}

		log.Infof(" -> Destroying SSHKey %s[%d]...", key.Name, key.ID)

		if _, err := d.getClient().SSHKey.Delete(context.Background(), key); err != nil {
			return errors.Wrap(err, "could not delete ssh key")
		}
	}

	return nil
}

func (d *Driver) Restart() error {
	srv, err := d.getServerHandle()
	if err != nil {
		return errors.Wrap(err, "could not get server handle")
	}
	if srv == nil {
		return errors.New("server not found")
	}

	act, _, err := d.getClient().Server.Reboot(context.Background(), srv)
	if err != nil {
		return errors.Wrap(err, "could not reboot server")
	}

	log.Infof(" -> Rebooting server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Start() error {
	srv, err := d.getServerHandle()
	if err != nil {
		return errors.Wrap(err, "could not get server handle")
	}
	if srv == nil {
		return errors.New("server not found")
	}

	act, _, err := d.getClient().Server.Poweron(context.Background(), srv)
	if err != nil {
		return errors.Wrap(err, "could not power on server")
	}

	log.Infof(" -> Starting server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Stop() error {
	srv, err := d.getServerHandle()
	if err != nil {
		return errors.Wrap(err, "could not get server handle")
	}
	if srv == nil {
		return errors.New("server not found")
	}

	act, _, err := d.getClient().Server.Shutdown(context.Background(), srv)
	if err != nil {
		return errors.Wrap(err, "could not shutdown server")
	}

	log.Infof(" -> Shutting down server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) Kill() error {
	srv, err := d.getServerHandle()
	if err != nil {
		return errors.Wrap(err, "could not get server handle")
	}
	if srv == nil {
		return errors.New("server not found")
	}

	act, _, err := d.getClient().Server.Poweroff(context.Background(), srv)
	if err != nil {
		return errors.Wrap(err, "could not poweroff server")
	}

	log.Infof(" -> Powering off server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}

func (d *Driver) getClient() *hcloud.Client {
	return hcloud.NewClient(hcloud.WithToken(d.AccessToken), hcloud.WithApplication("docker-machine-driver", Version))
}

func (d *Driver) copySSHKeyPair(src string) error {
	if err := mcnutils.CopyFile(src, d.GetSSHKeyPath()); err != nil {
		return errors.Wrap(err, "could not copy ssh key")
	}

	if err := mcnutils.CopyFile(src+".pub", d.GetSSHKeyPath()+".pub"); err != nil {
		return errors.Wrap(err, "could not copy ssh public key")
	}

	if err := os.Chmod(d.GetSSHKeyPath(), 0600); err != nil {
		return errors.Wrap(err, "could not set permissions on the ssh key")
	}

	return nil
}

func (d *Driver) getLocation() (*hcloud.Location, error) {
	if d.cachedLocation != nil {
		return d.cachedLocation, nil
	}

	location, _, err := d.getClient().Location.GetByName(context.Background(), d.Location)
	if err != nil {
		return location, errors.Wrap(err, "could not get location by name")
	}
	d.cachedLocation = location
	return location, nil
}

func (d *Driver) getType() (*hcloud.ServerType, error) {
	if d.cachedType != nil {
		return d.cachedType, nil
	}

	stype, _, err := d.getClient().ServerType.GetByName(context.Background(), d.Type)
	if err != nil {
		return stype, errors.Wrap(err, "could not get type by name")
	}
	d.cachedType = stype
	return stype, nil
}

func (d *Driver) getImage() (*hcloud.Image, error) {
	if d.cachedImage != nil {
		return d.cachedImage, nil
	}

	var image *hcloud.Image
	var err error

	if d.ImageID != 0 {
		image, _, err = d.getClient().Image.GetByID(context.Background(), d.ImageID)
		if err != nil {
			return image, errors.Wrap(err, fmt.Sprintf("could not get image by id %v", d.ImageID))
		}
	} else {
		image, _, err = d.getClient().Image.GetByName(context.Background(), d.Image)
		if err != nil {
			return image, errors.Wrap(err, fmt.Sprintf("could not get image by name %v", d.Image))
		}
	}

	d.cachedImage = image
	return image, nil
}

func (d *Driver) getKey() (*hcloud.SSHKey, error) {
	if d.cachedKey != nil {
		return d.cachedKey, nil
	}

	stype, _, err := d.getClient().SSHKey.GetByID(context.Background(), d.KeyID)
	if err != nil {
		return stype, errors.Wrap(err, "could not get sshkey by ID")
	}
	d.cachedKey = stype
	return stype, nil
}

func (d *Driver) getRemoteKeyWithSameFingerprint(pubkey_byte []byte) (*hcloud.SSHKey, error) {
	pubkey, _, _, _, err := ssh.ParseAuthorizedKey(pubkey_byte)
	if err != nil {
		return nil, errors.Wrap(err, "could not parse ssh public key")
	}

	fp := ssh.FingerprintLegacyMD5(pubkey)

	remotekey, _, err := d.getClient().SSHKey.GetByFingerprint(context.Background(), fp)
	if err != nil {
		return remotekey, errors.Wrap(err, "could not get sshkey by fingerprint")
	}
	return remotekey, nil
}

func (d *Driver) getServerHandle() (*hcloud.Server, error) {
	if d.cachedServer != nil {
		return d.cachedServer, nil
	}

	if d.ServerID == 0 {
		return nil, errors.New("server ID was 0")
	}

	srv, _, err := d.getClient().Server.GetByID(context.Background(), d.ServerID)
	if err != nil {
		return nil, errors.Wrap(err, "could not get client by ID")
	}

	d.cachedServer = srv
	return srv, nil
}

func (d *Driver) waitForAction(a *hcloud.Action) error {
	for {
		act, _, err := d.getClient().Action.GetByID(context.Background(), a.ID)
		if err != nil {
			return errors.Wrap(err, "could not get client by ID")
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
