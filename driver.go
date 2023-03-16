package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
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

// Driver contains hetzner-specific data to implement [drivers.Driver]
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
	dangling          []func()
	ServerID          int
	cachedServer      *hcloud.Server
	userData          string
	userDataFile      string
	Volumes           []string
	Networks          []string
	UsePrivateNetwork bool
	DisablePublic4    bool
	DisablePublic6    bool
	PrimaryIPv4       string
	cachedPrimaryIPv4 *hcloud.PrimaryIP
	PrimaryIPv6       string
	cachedPrimaryIPv6 *hcloud.PrimaryIP
	Firewalls         []string
	ServerLabels      map[string]string
	keyLabels         map[string]string
	placementGroup    string
	cachedPGrp        *hcloud.PlacementGroup

	AdditionalKeys       []string
	AdditionalKeyIDs     []int
	cachedAdditionalKeys []*hcloud.SSHKey

	WaitOnError int
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
	flagUserDataFile      = "hetzner-user-data-file"
	flagVolumes           = "hetzner-volumes"
	flagNetworks          = "hetzner-networks"
	flagUsePrivateNetwork = "hetzner-use-private-network"
	flagDisablePublic4    = "hetzner-disable-public-ipv4"
	flagDisablePublic6    = "hetzner-disable-public-ipv6"
	flagPrimary4          = "hetzner-primary-ipv4"
	flagPrimary6          = "hetzner-primary-ipv6"
	flagDisablePublic     = "hetzner-disable-public"
	flagFirewalls         = "hetzner-firewalls"
	flagAdditionalKeys    = "hetzner-additional-key"
	flagServerLabel       = "hetzner-server-label"
	flagKeyLabel          = "hetzner-key-label"
	flagPlacementGroup    = "hetzner-placement-group"
	flagAutoSpread        = "hetzner-auto-spread"

	flagSshUser = "hetzner-ssh-user"
	flagSshPort = "hetzner-ssh-port"

	labelNamespace    = "docker-machine"
	labelAutoSpreadPg = "auto-spread"
	labelAutoCreated  = "auto-created"

	autoSpreadPgName = "__auto_spread"

	defaultSSHPort = 22
	defaultSSHUser = "root"

	flagWaitOnError    = "hetzner-wait-on-error"
	defaultWaitOnError = 0

	legacyFlagUserDataFromFile = "hetzner-user-data-from-file"
	legacyFlagDisablePublic4   = "hetzner-disable-public-4"
	legacyFlagDisablePublic6   = "hetzner-disable-public-6"
)

// NewDriver initializes a new driver instance; see [drivers.Driver.NewDriver]
func NewDriver() *Driver {
	return &Driver{
		Type:          defaultType,
		IsExistingKey: false,
		BaseDriver:    &drivers.BaseDriver{},
	}
}

// DriverName returns the hard-coded string "hetzner"; see [drivers.Driver.DriverName]
func (d *Driver) DriverName() string {
	return "hetzner"
}

// GetCreateFlags retrieves additional driver-specific arguments; see [drivers.Driver.GetCreateFlags]
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
			Value:  "",
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
			Usage:  "Cloud-init based user data (inline).",
			Value:  "",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_USER_DATA_FROM_FILE",
			Name:   legacyFlagUserDataFromFile,
			Usage:  "DEPRECATED, use --hetzner-user-data-file. Treat --hetzner-user-data argument as filename.",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_USER_DATA_FILE",
			Name:   flagUserDataFile,
			Usage:  "Cloud-init based user data (read from file)",
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
			EnvVar: "HETZNER_DISABLE_PUBLIC_IPV4",
			Name:   flagDisablePublic4,
			Usage:  "Disable public ipv4",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC_4",
			Name:   legacyFlagDisablePublic4,
			Usage:  "DEPRECATED, use --hetzner-disable-public-ipv4; disable public ipv4",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC_IPV6",
			Name:   flagDisablePublic6,
			Usage:  "Disable public ipv6",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC_6",
			Name:   legacyFlagDisablePublic6,
			Usage:  "DEPRECATED, use --hetzner-disable-public-ipv6; disable public ipv6",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC",
			Name:   flagDisablePublic,
			Usage:  "Disable public ip (v4 & v6)",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_PRIMARY_IPV4",
			Name:   flagPrimary4,
			Usage:  "Existing primary IPv4 address",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_PRIMARY_IPV6",
			Name:   flagPrimary6,
			Usage:  "Existing primary IPv6 address",
			Value:  "",
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_FIREWALLS",
			Name:   flagFirewalls,
			Usage:  "Firewall IDs or names which should be applied on the server",
			Value:  []string{},
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_ADDITIONAL_KEYS",
			Name:   flagAdditionalKeys,
			Usage:  "Additional public keys to be attached to the server",
			Value:  []string{},
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_SERVER_LABELS",
			Name:   flagServerLabel,
			Usage:  "Key value pairs of additional labels to assign to the server",
			Value:  []string{},
		},
		mcnflag.StringSliceFlag{
			EnvVar: "HETZNER_KEY_LABELS",
			Name:   flagKeyLabel,
			Usage:  "Key value pairs of additional labels to assign to the SSH key",
			Value:  []string{},
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_PLACEMENT_GROUP",
			Name:   flagPlacementGroup,
			Usage:  "Placement group ID or name to add the server to; will be created if it does not exist",
			Value:  "",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_AUTO_SPREAD",
			Name:   flagAutoSpread,
			Usage:  "Auto-spread on a docker-machine-specific default placement group",
		},
		mcnflag.StringFlag{
			EnvVar: "HETZNER_SSH_USER",
			Name:   flagSshUser,
			Usage:  "SSH username",
			Value:  defaultSSHUser,
		},
		mcnflag.IntFlag{
			EnvVar: "HETZNER_SSH_PORT",
			Name:   flagSshPort,
			Usage:  "SSH port",
			Value:  defaultSSHPort,
		},
		mcnflag.IntFlag{
			EnvVar: "HETZNER_WAIT_ON_ERROR",
			Name:   flagWaitOnError,
			Usage:  "Wait if an error happens while creating the server",
			Value:  defaultWaitOnError,
		},
	}
}

// SetConfigFromFlags handles additional driver arguments as retrieved by [Driver.GetCreateFlags];
// see [drivers.Driver.SetConfigFromFlags]
func (d *Driver) SetConfigFromFlags(opts drivers.DriverOptions) error {
	return d.setConfigFromFlags(opts)
}

func (d *Driver) setConfigFromFlagsImpl(opts drivers.DriverOptions) error {
	d.AccessToken = opts.String(flagAPIToken)
	d.Image = opts.String(flagImage)
	d.ImageID = opts.Int(flagImageID)
	d.Location = opts.String(flagLocation)
	d.Type = opts.String(flagType)
	d.KeyID = opts.Int(flagExKeyID)
	d.IsExistingKey = d.KeyID != 0
	d.originalKey = opts.String(flagExKeyPath)
	err := d.setUserDataFlags(opts)
	if err != nil {
		return err
	}
	d.Volumes = opts.StringSlice(flagVolumes)
	d.Networks = opts.StringSlice(flagNetworks)
	disablePublic := opts.Bool(flagDisablePublic)
	d.UsePrivateNetwork = opts.Bool(flagUsePrivateNetwork) || disablePublic
	d.DisablePublic4 = d.deprecatedBooleanFlag(opts, flagDisablePublic4, legacyFlagDisablePublic4) || disablePublic
	d.DisablePublic6 = d.deprecatedBooleanFlag(opts, flagDisablePublic6, legacyFlagDisablePublic6) || disablePublic
	d.PrimaryIPv4 = opts.String(flagPrimary4)
	d.PrimaryIPv6 = opts.String(flagPrimary6)
	d.Firewalls = opts.StringSlice(flagFirewalls)
	d.AdditionalKeys = opts.StringSlice(flagAdditionalKeys)

	d.SSHUser = opts.String(flagSshUser)
	d.SSHPort = opts.Int(flagSshPort)

	d.WaitOnError = opts.Int(flagWaitOnError)

	d.placementGroup = opts.String(flagPlacementGroup)
	if opts.Bool(flagAutoSpread) {
		if d.placementGroup != "" {
			return d.flagFailure("%v and %v are mutually exclusive", flagAutoSpread, flagPlacementGroup)
		}
		d.placementGroup = autoSpreadPgName
	}

	err = d.setLabelsFromFlags(opts)
	if err != nil {
		return err
	}

	d.SetSwarmConfigFromFlags(opts)

	if d.AccessToken == "" {
		return d.flagFailure("hetzner requires --%v to be set", flagAPIToken)
	}

	if d.ImageID != 0 && d.Image != "" && d.Image != defaultImage /* support legacy behaviour */ {
		return d.flagFailure("--%v and --%v are mutually exclusive", flagImage, flagImageID)
	} else if d.ImageID == 0 && d.Image == "" {
		d.Image = defaultImage
	}

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

	instrumented(d)

	return nil
}

func (d *Driver) deprecatedBooleanFlag(opts drivers.DriverOptions, flag, deprecatedFlag string) bool {
	if opts.Bool(deprecatedFlag) {
		log.Warnf("--%v is deprecated, use --%v instead", deprecatedFlag, flag)
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

		log.Warnf("--%v is deprecated, pass '--%v \"%v\"'", legacyFlagUserDataFromFile, flagUserDataFile, userData)
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

// GetSSHUsername retrieves the SSH username used to connect to the server during provisioning
func (d *Driver) GetSSHUsername() string {
	return d.SSHUser
}

// GetSSHPort retrieves the port used to connect to the server during provisioning
func (d *Driver) GetSSHPort() (int, error) {
	return d.SSHPort, nil
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
			return errors.Errorf("key label %v is not in key=value format", label)
		}
		d.keyLabels[split[0]] = split[1]
	}
	return nil
}

// PreCreateCheck validates the Driver data is in a valid state for creation; see [drivers.Driver.PreCreateCheck]
func (d *Driver) PreCreateCheck() error {
	if d.IsExistingKey {
		if d.originalKey == "" {
			return d.flagFailure("specifying an existing key ID requires the existing key path to be set as well")
		}

		key, err := d.getKey()
		if err != nil {
			return errors.Wrap(err, "could not get key")
		}

		buf, err := os.ReadFile(d.originalKey + ".pub")
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

	if _, err := d.getPlacementGroup(); err != nil {
		return fmt.Errorf("could not create placement group: %w", err)
	}

	if _, err := d.getPrimaryIPv4(); err != nil {
		return fmt.Errorf("could not resolve primary IPv4: %w", err)
	}

	if _, err := d.getPrimaryIPv6(); err != nil {
		return fmt.Errorf("could not resolve primary IPv6: %w", err)
	}

	if d.UsePrivateNetwork && len(d.Networks) == 0 {
		return errors.Errorf("No private network attached.")
	}

	return nil
}

// Create actually creates the hetzner-cloud server; see [drivers.Driver.Create]
func (d *Driver) Create() error {
	err := d.prepareLocalKey()
	if err != nil {
		return err
	}

	defer d.destroyDangling()
	err = d.createRemoteKeys()
	if err != nil {
		return err
	}

	log.Infof("Creating Hetzner server...")

	srvopts, err := d.makeCreateServerOptions()
	if err != nil {
		return err
	}

	srv, _, err := d.getClient().Server.Create(context.Background(), instrumented(*srvopts))
	if err != nil {
		time.Sleep(time.Duration(d.WaitOnError) * time.Second)
		return errors.Wrap(err, "could not create server")
	}

	log.Infof(" -> Creating server %s[%d] in %s[%d]", srv.Server.Name, srv.Server.ID, srv.Action.Command, srv.Action.ID)
	if err = d.waitForAction(srv.Action); err != nil {
		return errors.Wrap(err, "could not wait for action")
	}

	d.ServerID = srv.Server.ID
	log.Infof(" -> Server %s[%d]: Waiting to come up...", srv.Server.Name, srv.Server.ID)

	err = d.waitForRunningServer()
	if err != nil {
		return err
	}

	err = d.configureNetworkAccess(srv)
	if err != nil {
		return err
	}

	log.Infof(" -> Server %s[%d] ready. Ip %s", srv.Server.Name, srv.Server.ID, d.IPAddress)
	// Successful creation, so no keys dangle anymore
	d.dangling = nil

	return nil
}

func (d *Driver) configureNetworkAccess(srv hcloud.ServerCreateResult) error {
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
	} else if d.DisablePublic4 {
		log.Infof("Using public IPv6 network ...")

		pv6 := srv.Server.PublicNet.IPv6
		ip := pv6.IP
		if ip.Mask(pv6.Network.Mask).Equal(pv6.Network.IP) { // no host given
			ip[net.IPv6len-1] |= 0x01 // TODO make this configurable
		}

		ips := ip.String()
		log.Infof(" -> resolved %v ...", ips)
		d.IPAddress = ips
	} else {
		log.Infof("Using public network ...")
		d.IPAddress = srv.Server.PublicNet.IPv4.IP.String()
	}
	return nil
}

func (d *Driver) waitForRunningServer() error {
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
	return nil
}

func (d *Driver) makeCreateServerOptions() (*hcloud.ServerCreateOpts, error) {
	pgrp, err := d.getPlacementGroup()
	if err != nil {
		return nil, err
	}

	userData, err := d.getUserData()
	if err != nil {
		return nil, err
	}

	srvopts := hcloud.ServerCreateOpts{
		Name:           d.GetMachineName(),
		UserData:       userData,
		Labels:         d.ServerLabels,
		PlacementGroup: pgrp,
	}

	err = d.setPublicNetIfRequired(&srvopts)
	if err != nil {
		return nil, err
	}

	networks, err := d.createNetworks()
	if err != nil {
		return nil, err
	}
	srvopts.Networks = networks

	firewalls, err := d.createFirewalls()
	if err != nil {
		return nil, err
	}
	srvopts.Firewalls = firewalls

	volumes, err := d.createVolumes()
	if err != nil {
		return nil, err
	}
	srvopts.Volumes = volumes

	if srvopts.Location, err = d.getLocation(); err != nil {
		return nil, errors.Wrap(err, "could not get location")
	}
	if srvopts.ServerType, err = d.getType(); err != nil {
		return nil, errors.Wrap(err, "could not get type")
	}
	if srvopts.Image, err = d.getImage(); err != nil {
		return nil, errors.Wrap(err, "could not get image")
	}
	key, err := d.getKey()
	if err != nil {
		return nil, errors.Wrap(err, "could not get ssh key")
	}
	srvopts.SSHKeys = append(d.cachedAdditionalKeys, key)
	return &srvopts, nil
}

func (d *Driver) getUserData() (string, error) {
	file := d.userDataFile
	if file == "" {
		return d.userData, nil
	}

	readUserData, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(readUserData), nil
}

func (d *Driver) setPublicNetIfRequired(srvopts *hcloud.ServerCreateOpts) error {
	pip4, err := d.getPrimaryIPv4()
	if err != nil {
		return err
	}
	pip6, err := d.getPrimaryIPv6()
	if err != nil {
		return err
	}

	if d.DisablePublic4 || d.DisablePublic6 || pip4 != nil || pip6 != nil {
		srvopts.PublicNet = &hcloud.ServerCreatePublicNet{
			EnableIPv4: !d.DisablePublic4 || pip4 != nil,
			EnableIPv6: !d.DisablePublic6 || pip6 != nil,
			IPv4:       pip4,
			IPv6:       pip6,
		}
	}
	return nil
}

func (d *Driver) createNetworks() ([]*hcloud.Network, error) {
	networks := []*hcloud.Network{}
	for _, networkIDorName := range d.Networks {
		network, _, err := d.getClient().Network.Get(context.Background(), networkIDorName)
		if err != nil {
			return nil, errors.Wrap(err, "could not get network by ID or name")
		}
		if network == nil {
			return nil, errors.Errorf("network '%s' not found", networkIDorName)
		}
		networks = append(networks, network)
	}
	return instrumented(networks), nil
}

func (d *Driver) createFirewalls() ([]*hcloud.ServerCreateFirewall, error) {
	firewalls := []*hcloud.ServerCreateFirewall{}
	for _, firewallIDorName := range d.Firewalls {
		firewall, _, err := d.getClient().Firewall.Get(context.Background(), firewallIDorName)
		if err != nil {
			return nil, errors.Wrap(err, "could not get firewall by ID or name")
		}
		if firewall == nil {
			return nil, errors.Errorf("firewall '%s' not found", firewallIDorName)
		}
		firewalls = append(firewalls, &hcloud.ServerCreateFirewall{Firewall: *firewall})
	}
	return instrumented(firewalls), nil
}

func (d *Driver) createVolumes() ([]*hcloud.Volume, error) {
	volumes := []*hcloud.Volume{}
	for _, volumeIDorName := range d.Volumes {
		volume, _, err := d.getClient().Volume.Get(context.Background(), volumeIDorName)
		if err != nil {
			return nil, errors.Wrap(err, "could not get volume by ID or name")
		}
		if volume == nil {
			return nil, errors.Errorf("volume '%s' not found", volumeIDorName)
		}
		volumes = append(volumes, volume)
	}
	return instrumented(volumes), nil
}

func (d *Driver) createRemoteKeys() error {
	if d.KeyID == 0 {
		log.Infof("Creating SSH key...")

		buf, err := os.ReadFile(d.GetSSHKeyPath() + ".pub")
		if err != nil {
			return errors.Wrap(err, "could not read ssh public key")
		}

		key, err := d.getRemoteKeyWithSameFingerprint(buf)
		if err != nil {
			return errors.Wrap(err, "error retrieving potentially existing key")
		}
		if key == nil {
			log.Infof("SSH key not found in Hetzner. Uploading...")

			key, err = d.makeKey(d.GetMachineName(), string(buf), d.keyLabels)
			if err != nil {
				return err
			}
		} else {
			d.IsExistingKey = true
			log.Debugf("SSH key found in Hetzner. ID: %d", key.ID)
		}

		d.KeyID = key.ID
	}
	for i, pubkey := range d.AdditionalKeys {
		key, err := d.getRemoteKeyWithSameFingerprint([]byte(pubkey))
		if err != nil {
			return errors.Wrapf(err, "error checking for existing key for %v", pubkey)
		}
		if key == nil {
			log.Infof("Creating new key for %v...", pubkey)
			key, err = d.makeKey(fmt.Sprintf("%v-additional-%d", d.GetMachineName(), i), pubkey, d.keyLabels)

			if err != nil {
				return errors.Wrapf(err, "error creating new key for %v", pubkey)
			}

			log.Infof(" -> Created %v", key.ID)
			d.AdditionalKeyIDs = append(d.AdditionalKeyIDs, key.ID)
		} else {
			log.Infof("Using existing key (%v) %v", key.ID, key.Name)
		}

		d.cachedAdditionalKeys = append(d.cachedAdditionalKeys, key)
	}
	return nil
}

func (d *Driver) prepareLocalKey() error {
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
	return nil
}

// Creates a new key for the machine and appends it to the dangling key list
func (d *Driver) makeKey(name string, pubkey string, labels map[string]string) (*hcloud.SSHKey, error) {
	keyopts := hcloud.SSHKeyCreateOpts{
		Name:      name,
		PublicKey: pubkey,
		Labels:    labels,
	}

	key, _, err := d.getClient().SSHKey.Create(context.Background(), instrumented(keyopts))
	if err != nil {
		return nil, errors.Wrap(err, "could not create ssh key")
	} else if key == nil {
		return nil, errors.Errorf("key upload did not return an error, but key was nil")
	}

	d.dangling = append(d.dangling, func() {
		_, err := d.getClient().SSHKey.Delete(context.Background(), key)
		if err != nil {
			log.Error(fmt.Errorf("could not delete ssh key: %w", err))
		}
	})

	return key, nil
}

func (d *Driver) destroyDangling() {
	for _, destructor := range d.dangling {
		destructor()
	}
}

// GetSSHHostname retrieves the SSH host to connect to the machine; see [drivers.Driver.GetSSHHostname]
func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// GetURL retrieves the URL of the docker daemon on the machine; see [drivers.Driver.GetURL]
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

// GetState retrieves the state the machine is currently in; see [drivers.Driver.GetState]
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

// Remove deletes the hetzner server and additional resources created during creation; see [drivers.Driver.Remove]
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

			// failure to remove a placement group is not a hard error
			if softErr := d.removeEmptyServerPlacementGroup(srv); softErr != nil {
				log.Error(softErr)
			}
		}
	}

	// failure to remove a key is not ha hard error
	for i, id := range d.AdditionalKeyIDs {
		log.Infof(" -> Destroying additional key #%d (%d)", i, id)
		key, _, softErr := d.getClient().SSHKey.GetByID(context.Background(), id)
		if softErr != nil {
			log.Warnf(" ->  -> could not retrieve key %v", softErr)
		} else if key == nil {
			log.Warnf(" ->  -> %d no longer exists", id)
		}

		_, softErr = d.getClient().SSHKey.Delete(context.Background(), key)
		if softErr != nil {
			log.Warnf(" ->  -> could not remove key: %v", softErr)
		}
	}

	// failure to remove a server-specific key is a hard error
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

// Restart instructs the hetzner cloud server to reboot; see [drivers.Driver.Restart]
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

// Start instructs the hetzner cloud server to power up; see [drivers.Driver.Start]
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

// Stop instructs the hetzner cloud server to shut down; see [drivers.Driver.Stop]
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

// Kill forcefully shuts down the hetzner cloud server; see [drivers.Driver.Kill]
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
	return hcloud.NewClient(hcloud.WithToken(d.AccessToken), hcloud.WithApplication("docker-machine-driver", version))
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
	return instrumented(stype), nil
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
	return instrumented(image), nil
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
	return instrumented(stype), nil
}

func (d *Driver) getRemoteKeyWithSameFingerprint(publicKeyBytes []byte) (*hcloud.SSHKey, error) {
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKeyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "could not parse ssh public key")
	}

	fp := ssh.FingerprintLegacyMD5(publicKey)

	remoteKey, _, err := d.getClient().SSHKey.GetByFingerprint(context.Background(), fp)
	if err != nil {
		return remoteKey, errors.Wrap(err, "could not get sshkey by fingerprint")
	}
	return instrumented(remoteKey), nil
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

func (d *Driver) labelName(name string) string {
	return labelNamespace + "/" + name
}

func (d *Driver) getAutoPlacementGroup() (*hcloud.PlacementGroup, error) {
	res, err := d.getClient().PlacementGroup.AllWithOpts(context.Background(), hcloud.PlacementGroupListOpts{
		ListOpts: hcloud.ListOpts{LabelSelector: d.labelName(labelAutoSpreadPg)},
	})

	if err != nil {
		return nil, err
	}

	if len(res) != 0 {
		return res[0], nil
	}

	grp, err := d.makePlacementGroup("Docker-Machine auto spread", map[string]string{
		d.labelName(labelAutoSpreadPg): "true",
		d.labelName(labelAutoCreated):  "true",
	})

	return instrumented(grp), err
}

func (d *Driver) makePlacementGroup(name string, labels map[string]string) (*hcloud.PlacementGroup, error) {
	grp, _, err := d.getClient().PlacementGroup.Create(context.Background(), instrumented(hcloud.PlacementGroupCreateOpts{
		Name:   name,
		Labels: labels,
		Type:   "spread",
	}))

	if grp.PlacementGroup != nil {
		d.dangling = append(d.dangling, func() {
			_, err := d.getClient().PlacementGroup.Delete(context.Background(), grp.PlacementGroup)
			if err != nil {
				log.Errorf("could not delete placement group: %v", err)
			}
		})
	}

	if err != nil {
		return nil, fmt.Errorf("could not create placement group: %w", err)
	}

	return instrumented(grp.PlacementGroup), nil
}

func (d *Driver) getPlacementGroup() (*hcloud.PlacementGroup, error) {
	if d.placementGroup == "" {
		return nil, nil
	} else if d.cachedPGrp != nil {
		return d.cachedPGrp, nil
	}

	name := d.placementGroup
	if name == autoSpreadPgName {
		grp, err := d.getAutoPlacementGroup()
		d.cachedPGrp = grp
		return grp, err
	} else {
		client := d.getClient().PlacementGroup
		grp, _, err := client.Get(context.Background(), name)
		if err != nil {
			return nil, fmt.Errorf("could not get placement group: %w", err)
		}

		if grp != nil {
			return grp, nil
		}

		return d.makePlacementGroup(name, map[string]string{d.labelName(labelAutoCreated): "true"})
	}
}

func (d *Driver) removeEmptyServerPlacementGroup(srv *hcloud.Server) error {
	pg := srv.PlacementGroup
	if pg == nil {
		return nil
	}

	if len(pg.Servers) > 1 {
		log.Debugf("more than 1 servers in group, ignoring %v", pg)
		return nil
	}

	if auto, exists := pg.Labels[d.labelName(labelAutoCreated)]; exists && auto == "true" {
		_, err := d.getClient().PlacementGroup.Delete(context.Background(), pg)
		if err != nil {
			return fmt.Errorf("could not remove placement group: %w", err)
		}
		return nil
	} else {
		log.Debugf("group not auto-created, ignoring: %v", pg)
		return nil
	}
}

func (d *Driver) getPrimaryIPv4() (*hcloud.PrimaryIP, error) {
	raw := d.PrimaryIPv4
	if raw == "" {
		return nil, nil
	} else if d.cachedPrimaryIPv4 != nil {
		return d.cachedPrimaryIPv4, nil
	}

	ip, err := d.resolvePrimaryIP(raw)
	d.cachedPrimaryIPv4 = ip
	return ip, err
}

func (d *Driver) getPrimaryIPv6() (*hcloud.PrimaryIP, error) {
	raw := d.PrimaryIPv6
	if raw == "" {
		return nil, nil
	} else if d.cachedPrimaryIPv6 != nil {
		return d.cachedPrimaryIPv6, nil
	}

	ip, err := d.resolvePrimaryIP(raw)
	d.cachedPrimaryIPv6 = ip
	return ip, err
}

func (d *Driver) resolvePrimaryIP(raw string) (*hcloud.PrimaryIP, error) {
	client := d.getClient().PrimaryIP

	var getter func(context.Context, string) (*hcloud.PrimaryIP, *hcloud.Response, error)
	if net.ParseIP(raw) != nil {
		getter = client.GetByIP
	} else {
		getter = client.Get
	}

	ip, _, err := getter(context.Background(), raw)

	if err != nil {
		return nil, fmt.Errorf("could not get primary IP: %w", err)
	}

	if ip != nil {
		return instrumented(ip), nil
	}

	return nil, fmt.Errorf("primary IP not found: %v", raw)
}
