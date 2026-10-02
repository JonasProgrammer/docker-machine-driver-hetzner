package driver

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/state"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

func (d *Driver) waitForRunningServer() error {
	start_time := time.Now()
	for {
		srvstate, err := d.GetState()
		if err != nil {
			return fmt.Errorf("could not get state: %w", err)
		}

		if srvstate == state.Running {
			break
		}

		elapsed_time := time.Since(start_time).Seconds()
		if d.WaitForRunningTimeout > 0 && int(elapsed_time) > d.WaitForRunningTimeout {
			return fmt.Errorf("server exceeded wait-for-running-timeout")
		}

		time.Sleep(time.Duration(d.WaitOnPolling) * time.Second)
	}
	return nil
}

func (d *Driver) waitForInitialStartup(srv hcloud.ServerCreateResult) error {
	if len(srv.NextActions) != 0 {
		if err := d.waitForMultipleActions("server.NextActions", srv.NextActions); err != nil {
			return fmt.Errorf("could not wait for NextActions: %w", err)
		}
	}

	return d.waitForRunningServer()
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

	networks, deferredNetworks, err := d.createNetworks()
	if err != nil {
		return nil, err
	}
	srvopts.Networks = networks
	d.deferredNetworks = deferredNetworks
	d.temporaryIPv6 = len(deferredNetworks) > 0 && requiresTemporaryPublicIPv6(&srvopts)
	if len(deferredNetworks) > 0 {
		srvopts.StartAfterCreate = hcloud.Ptr(false)
	}
	if d.temporaryIPv6 {
		srvopts.PublicNet.EnableIPv6 = true
	}

	firewalls, err := d.createFirewalls()
	if err != nil {
		return nil, err
	}
	if d.temporaryIPv6 && len(firewalls) > 0 {
		log.Warnf("firewalls are not applied, since the server has no public network")
	} else {
		srvopts.Firewalls = firewalls
	}

	volumes, err := d.createVolumes()
	if err != nil {
		return nil, err
	}
	srvopts.Volumes = volumes

	if srvopts.Location, err = d.getLocationNullable(); err != nil {
		return nil, fmt.Errorf("could not get location: %w", err)
	}
	if srvopts.ServerType, err = d.getType(); err != nil {
		return nil, fmt.Errorf("could not get type: %w", err)
	}
	if srvopts.Image, err = d.getImage(); err != nil {
		return nil, fmt.Errorf("could not get image: %w", err)
	}
	key, err := d.getKey()
	if err != nil {
		return nil, fmt.Errorf("could not get ssh key: %w", err)
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

func (d *Driver) createNetworks() ([]*hcloud.Network, []networkAttachment, error) {
	networks := []*hcloud.Network{}
	deferred := []networkAttachment{}
	for _, raw := range d.Networks {
		spec := parseNetworkSpec(raw)
		network, _, err := d.getClient().Network.Get(context.Background(), spec.IDOrName)
		if err != nil {
			return nil, nil, fmt.Errorf("could not get network by ID or name: %w", err)
		}
		if network == nil {
			return nil, nil, fmt.Errorf("network '%s' not found", spec.IDOrName)
		}

		if spec.IPRange == nil {
			networks = append(networks, network)
			continue
		}

		if err = verifySubnetExists(network, spec.IPRange); err != nil {
			return nil, nil, err
		}
		deferred = append(deferred, networkAttachment{Network: network, IPRange: spec.IPRange})
	}
	return instrumented(networks), instrumented(deferred), nil
}

func (d *Driver) createFirewalls() ([]*hcloud.ServerCreateFirewall, error) {
	firewalls := []*hcloud.ServerCreateFirewall{}
	for _, firewallIDorName := range d.Firewalls {
		firewall, _, err := d.getClient().Firewall.Get(context.Background(), firewallIDorName)
		if err != nil {
			return nil, fmt.Errorf("could not get firewall by ID or name: %w", err)
		}
		if firewall == nil {
			return nil, fmt.Errorf("firewall '%s' not found", firewallIDorName)
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
			return nil, fmt.Errorf("could not get volume by ID or name: %w", err)
		}
		if volume == nil {
			return nil, fmt.Errorf("volume '%s' not found", volumeIDorName)
		}
		volumes = append(volumes, volume)
	}
	return instrumented(volumes), nil
}
