package driver

import (
	"context"
	"fmt"
	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"time"
)

func (d *Driver) getClient() *hcloud.Client {
	return hcloud.NewClient(hcloud.WithToken(d.AccessToken), hcloud.WithApplication("docker-machine-driver", d.version))
}

func (d *Driver) getLocation() (*hcloud.Location, error) {
	if d.cachedLocation != nil {
		return d.cachedLocation, nil
	}

	location, _, err := d.getClient().Location.GetByName(context.Background(), d.Location)
	if err != nil {
		return nil, errors.Wrap(err, "could not get location by name")
	}
	if location == nil {
		return nil, fmt.Errorf("unknown location: %v", d.Location)
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
		return nil, errors.Wrap(err, "could not get type by name")
	}
	if stype == nil {
		return nil, fmt.Errorf("unknown server type: %v", d.Type)
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
			return nil, errors.Wrap(err, fmt.Sprintf("could not get image by id %v", d.ImageID))
		}
		if image == nil {
			return nil, fmt.Errorf("image id not found: %v", d.ImageID)
		}
	} else {
		arch, err := d.getImageArchitectureForLookup()
		if err != nil {
			return nil, errors.Wrap(err, "could not determine image architecture")
		}

		image, _, err = d.getClient().Image.GetByNameAndArchitecture(context.Background(), d.Image, arch)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("could not get image by name %v", d.Image))
		}
		if image == nil {
			return nil, fmt.Errorf("image not found: %v[%v]", d.Image, arch)
		}
	}

	d.cachedImage = image
	return instrumented(image), nil
}

func (d *Driver) getImageArchitectureForLookup() (hcloud.Architecture, error) {
	if d.ImageArch != emptyImageArchitecture {
		return d.ImageArch, nil
	}

	serverType, err := d.getType()
	if err != nil {
		return "", err
	}

	return serverType.Architecture, nil
}

func (d *Driver) getKey() (*hcloud.SSHKey, error) {
	if d.cachedKey != nil {
		return d.cachedKey, nil
	}

	key, _, err := d.getClient().SSHKey.GetByID(context.Background(), d.KeyID)
	if err != nil {
		return nil, errors.Wrap(err, "could not get sshkey by ID")
	}
	if key == nil {
		return nil, fmt.Errorf("key not found: %v", d.KeyID)
	}
	d.cachedKey = key
	return instrumented(key), nil
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
	if remoteKey == nil {
		return nil, fmt.Errorf("key not found by fingerprint: %v", fp)
	}
	return instrumented(remoteKey), nil
}

func (d *Driver) getServerHandle() (*hcloud.Server, error) {
	srv, err := d.getServerHandleNullable()
	if err != nil {
		return nil, err
	}
	if srv == nil {
		return nil, fmt.Errorf("server does not exist: %v", d.ServerID)
	}
	return srv, nil
}

func (d *Driver) getServerHandleNullable() (*hcloud.Server, error) {
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
		if act == nil {
			return fmt.Errorf("action not found: %v", a.ID)
		}

		if act.Status == hcloud.ActionStatusSuccess {
			log.Debugf(" -> finished %s[%d]", act.Command, act.ID)
			break
		} else if act.Status == hcloud.ActionStatusRunning {
			log.Debugf(" -> %s[%d]: %d %%", act.Command, act.ID, act.Progress)
		} else if act.Status == hcloud.ActionStatusError {
			return act.Error()
		}

		time.Sleep(time.Duration(d.WaitOnPolling) * time.Second)
	}
	return nil
}
