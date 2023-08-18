package driver

import (
	"context"
	"fmt"
	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const (
	labelAutoSpreadPg = "auto-spread"
	labelAutoCreated  = "auto-created"
	autoSpreadPgName  = "__auto_spread"
)

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
