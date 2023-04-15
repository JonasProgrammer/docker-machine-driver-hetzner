package driver

import (
	"context"
	"github.com/docker/machine/libmachine/state"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/pkg/errors"
	"os"
	"time"
)

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
