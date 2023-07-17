package driver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/state"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/pkg/errors"
)

// Driver contains hetzner-specific data to implement [drivers.Driver]
type Driver struct {
	*drivers.BaseDriver

	AccessToken       string
	Image             string
	ImageID           int
	ImageArch         hcloud.Architecture
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

	WaitOnError           int
	WaitOnPolling         int
	WaitForRunningTimeout int

	// internal housekeeping
	version string
	usesDfr bool
}

const (
	defaultImage = "ubuntu-20.04"
	defaultType  = "cx11"

	flagAPIToken          = "hetzner-api-token"
	flagImage             = "hetzner-image"
	flagImageID           = "hetzner-image-id"
	flagImageArch         = "hetzner-image-arch"
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

	defaultSSHPort = 22
	defaultSSHUser = "root"

	flagWaitOnError              = "hetzner-wait-on-error"
	defaultWaitOnError           = 0
	flagWaitOnPolling            = "hetzner-wait-on-polling"
	defaultWaitOnPolling         = 1
	flagWaitForRunningTimeout    = "hetzner-wait-for-running-timeout"
	defaultWaitForRunningTimeout = 0

	legacyFlagUserDataFromFile = "hetzner-user-data-from-file"
	legacyFlagDisablePublic4   = "hetzner-disable-public-4"
	legacyFlagDisablePublic6   = "hetzner-disable-public-6"

	emptyImageArchitecture = hcloud.Architecture("")
)

// NewDriver initializes a new driver instance; see [drivers.Driver.NewDriver]
func NewDriver(version string) *Driver {
	if runningInstrumented {
		instrumented("running instrument mode") // will be a no-op when not built with instrumentation
	}
	return &Driver{
		Type:          defaultType,
		IsExistingKey: false,
		BaseDriver:    &drivers.BaseDriver{},
		version:       version,
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
			EnvVar: "HETZNER_IMAGE_ARCH",
			Name:   flagImageArch,
			Usage:  "Image architecture for lookup to use for server creation",
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
			Usage:  "DEPRECATED, legacy.",
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
			Usage:  "DEPRECATED, legacy",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC_IPV6",
			Name:   flagDisablePublic6,
			Usage:  "Disable public ipv6",
		},
		mcnflag.BoolFlag{
			EnvVar: "HETZNER_DISABLE_PUBLIC_6",
			Name:   legacyFlagDisablePublic6,
			Usage:  "DEPRECATED, legacy",
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
		mcnflag.IntFlag{
			EnvVar: "HETZNER_WAIT_ON_POLLING",
			Name:   flagWaitOnPolling,
			Usage:  "Period for waiting between requests when waiting for some state to change",
			Value:  defaultWaitOnPolling,
		},
		mcnflag.IntFlag{
			EnvVar: "HETZNER_WAIT_FOR_RUNNING_TIMEOUT",
			Name:   flagWaitForRunningTimeout,
			Usage:  "Period for waiting for a machine to be running before failing",
			Value:  defaultWaitForRunningTimeout,
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
	err := d.setImageArch(opts.String(flagImageArch))
	if err != nil {
		return err
	}
	d.Location = opts.String(flagLocation)
	d.Type = opts.String(flagType)
	d.KeyID = opts.Int(flagExKeyID)
	d.IsExistingKey = d.KeyID != 0
	d.originalKey = opts.String(flagExKeyPath)
	err = d.setUserDataFlags(opts)
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
	d.WaitOnPolling = opts.Int(flagWaitOnPolling)
	d.WaitForRunningTimeout = opts.Int(flagWaitForRunningTimeout)

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

	if err = d.verifyImageFlags(); err != nil {
		return err
	}

	if err = d.verifyNetworkFlags(); err != nil {
		return err
	}

	instrumented(d)

	if d.usesDfr {
		log.Warn("!!!! BREAKING-V5 !!!!")
		log.Warn("your configuration uses deprecated flags and will stop working as-is from v5 onwards")
		log.Warn("check preceding output for 'DEPRECATED' log statements")
		log.Warn("!!!! /BREAKING-V5 !!!!")
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

// PreCreateCheck validates the Driver data is in a valid state for creation; see [drivers.Driver.PreCreateCheck]
func (d *Driver) PreCreateCheck() error {
	if err := d.setupExistingKey(); err != nil {
		return err
	}

	if serverType, err := d.getType(); err != nil {
		return errors.Wrap(err, "could not get type")
	} else if d.ImageArch != "" && serverType.Architecture != d.ImageArch {
		log.Warnf("supplied architecture %v differs from server architecture %v", d.ImageArch, serverType.Architecture)
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
	if err := d.destroyServer(); err != nil {
		return err
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

	act, _, err := d.getClient().Server.Poweroff(context.Background(), srv)
	if err != nil {
		return errors.Wrap(err, "could not poweroff server")
	}

	log.Infof(" -> Powering off server %s[%d] in %s[%d]...", srv.Name, srv.ID, act.Command, act.ID)

	return d.waitForAction(act)
}
