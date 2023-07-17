package driver

import (
	"context"
	"fmt"
	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/pkg/errors"
)

func (d *Driver) destroyDangling() {
	for _, destructor := range d.dangling {
		destructor()
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

func (d *Driver) destroyServer() error {
	if d.ServerID == 0 {
		return nil
	}

	srv, err := d.getServerHandleNullable()
	if err != nil {
		return errors.Wrap(err, "could not get server handle")
	}

	if srv == nil {
		log.Infof(" -> Server does not exist anymore")
	} else {
		log.Infof(" -> Destroying server %s[%d] in...", srv.Name, srv.ID)

		res, _, err := d.getClient().Server.DeleteWithResult(context.Background(), srv)
		if err != nil {
			return errors.Wrap(err, "could not delete server")
		}

		// failure to remove a placement group is not a hard error
		if softErr := d.removeEmptyServerPlacementGroup(srv); softErr != nil {
			log.Error(softErr)
		}

		// wait for the server to actually be deleted
		if err = d.waitForAction(res.Action); err != nil {
			return errors.Wrap(err, "could not wait for deletion")
		}
	}

	return nil
}
