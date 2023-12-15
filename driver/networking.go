package driver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

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

func (d *Driver) configureNetworkAccess(srv hcloud.ServerCreateResult) error {
	if d.UsePrivateNetwork {
		for {
			// we need to wait until network is attached
			log.Infof("Wait until private network attached ...")
			server, _, err := d.getClient().Server.GetByID(context.Background(), srv.Server.ID)
			if err != nil {
				return fmt.Errorf("could not get newly created server [%d]: %w", srv.Server.ID, err)
			}
			if server.PrivateNet != nil {
				d.IPAddress = server.PrivateNet[0].IP.String()
				break
			}
			time.Sleep(time.Duration(d.WaitOnPolling) * time.Second)
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
