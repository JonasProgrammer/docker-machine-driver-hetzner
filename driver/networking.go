package driver

import (
	"context"
	"fmt"
	"net"
	"strings"
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
			server, err := d.getServerHandle()
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

type networkSpec struct {
	IDOrName string
	IPRange  *net.IPNet
}

func parseNetworkSpec(raw string) networkSpec {
	if idx := strings.LastIndex(raw, ":"); idx > 0 {
		if ip, ipRange, err := net.ParseCIDR(raw[idx+1:]); err == nil && ip.Equal(ipRange.IP) {
			return networkSpec{IDOrName: raw[:idx], IPRange: ipRange}
		}
	}
	return networkSpec{IDOrName: raw}
}

func verifySubnetExists(network *hcloud.Network, ipRange *net.IPNet) error {
	for _, subnet := range network.Subnets {
		if subnet.IPRange != nil && subnet.IPRange.String() == ipRange.String() {
			return nil
		}
	}
	return fmt.Errorf("network '%s' has no subnet %s", network.Name, ipRange)
}

type networkAttachment struct {
	Network *hcloud.Network
	IPRange *net.IPNet
}

func (d *Driver) attachDeferredNetworks(srv hcloud.ServerCreateResult) error {
	if len(srv.NextActions) != 0 {
		if err := d.waitForMultipleActions("server.NextActions", srv.NextActions); err != nil {
			return fmt.Errorf("could not wait for NextActions: %w", err)
		}
	}

	server := srv.Server
	for _, attachment := range d.deferredNetworks {
		log.Infof(" -> Attaching network %s[%d] using subnet %s", attachment.Network.Name, attachment.Network.ID, attachment.IPRange)
		action, _, err := d.getClient().Server.AttachToNetwork(context.Background(), server, hcloud.ServerAttachToNetworkOpts{
			Network: attachment.Network,
			IPRange: attachment.IPRange,
		})
		if err != nil {
			return fmt.Errorf("could not attach network %s: %w", attachment.Network.Name, err)
		}
		if err = d.waitForAction(action); err != nil {
			return fmt.Errorf("could not wait for network attachment: %w", err)
		}
	}

	if d.temporaryIPv6 {
		if err := d.removeTemporaryPublicIPv6(server); err != nil {
			return err
		}
	}

	log.Infof(" -> Powering on server %s[%d]", server.Name, server.ID)
	action, _, err := d.getClient().Server.Poweron(context.Background(), server)
	if err != nil {
		return fmt.Errorf("could not power on server: %w", err)
	}
	if err = d.waitForAction(action); err != nil {
		return fmt.Errorf("could not wait for power on: %w", err)
	}
	return nil
}

func requiresTemporaryPublicIPv6(srvopts *hcloud.ServerCreateOpts) bool {
	return len(srvopts.Networks) == 0 && srvopts.PublicNet != nil && !srvopts.PublicNet.EnableIPv4 && !srvopts.PublicNet.EnableIPv6
}

func (d *Driver) findTemporaryPublicIPv6(server *hcloud.Server) (*hcloud.PrimaryIP, error) {
	if server.PublicNet.IPv6.ID != 0 {
		return &hcloud.PrimaryIP{ID: server.PublicNet.IPv6.ID}, nil
	}

	primaryIPs, err := d.getClient().PrimaryIP.All(context.Background())
	if err != nil {
		return nil, fmt.Errorf("could not list primary IPs: %w", err)
	}
	for _, primaryIP := range primaryIPs {
		if primaryIP.Type == hcloud.PrimaryIPTypeIPv6 && primaryIP.AssigneeType == "server" && primaryIP.AssigneeID == server.ID {
			return primaryIP, nil
		}
	}
	return nil, fmt.Errorf("could not determine temporary primary IPv6 of server %s[%d]", server.Name, server.ID)
}

func (d *Driver) removeTemporaryPublicIPv6(server *hcloud.Server) error {
	primaryIP, err := d.findTemporaryPublicIPv6(server)
	if err != nil {
		return err
	}

	log.Infof(" -> Removing temporary primary IPv6 %d", primaryIP.ID)
	action, _, err := d.getClient().PrimaryIP.Unassign(context.Background(), primaryIP.ID)
	if err != nil {
		return fmt.Errorf("could not unassign temporary primary IPv6 %d: %w", primaryIP.ID, err)
	}
	if err = d.waitForAction(action); err != nil {
		return fmt.Errorf("could not wait for unassigning temporary primary IPv6 %d: %w", primaryIP.ID, err)
	}

	if _, err = d.getClient().PrimaryIP.Delete(context.Background(), primaryIP); err != nil {
		log.Warnf("could not delete temporary primary IPv6 %d, it has to be deleted manually: %v", primaryIP.ID, err)
	}
	return nil
}
