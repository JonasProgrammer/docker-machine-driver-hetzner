package driver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/machine/libmachine/log"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/crypto/ssh"
)

func (d *Driver) getClient() *hcloud.Client {
	opts := []hcloud.ClientOption{
		hcloud.WithToken(d.AccessToken),
		hcloud.WithApplication("docker-machine-driver", d.version),
		hcloud.WithPollBackoffFunc(hcloud.ConstantBackoff(time.Duration(d.WaitOnPolling) * time.Second)),
	}

	opts = d.setupClientInstrumentation(opts)

	return hcloud.NewClient(opts...)
}

func (d *Driver) getLocationNullable() (*hcloud.Location, error) {
	if d.cachedLocation != nil {
		return d.cachedLocation, nil
	}
	if d.Location == "" {
		return nil, nil
	}

	location, _, err := d.getClient().Location.GetByName(context.Background(), d.Location)
	if err != nil {
		return nil, fmt.Errorf("could not get location by name: %w", err)
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
		return nil, fmt.Errorf("could not get type by name: %w", err)
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
			return nil, fmt.Errorf("could not get image by id %v: %w", d.ImageID, err)
		}
		if image == nil {
			return nil, fmt.Errorf("image id not found: %v", d.ImageID)
		}
	} else {
		arch, err := d.getImageArchitectureForLookup()
		if err != nil {
			return nil, fmt.Errorf("could not determine image architecture: %w", err)
		}

		image, _, err = d.getClient().Image.GetByNameAndArchitecture(context.Background(), d.Image, arch)
		if err != nil {
			return nil, fmt.Errorf("could not get image by name %v: %w", d.Image, err)
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
	key, err := d.getKeyNullable()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("key not found: %v", d.KeyID)
	}
	return key, err
}

func (d *Driver) getKeyNullable() (*hcloud.SSHKey, error) {
	if d.cachedKey != nil {
		return d.cachedKey, nil
	}

	key, _, err := d.getClient().SSHKey.GetByID(context.Background(), d.KeyID)
	if err != nil {
		return nil, fmt.Errorf("could not get sshkey by ID: %w", err)
	}
	d.cachedKey = key
	return instrumented(key), nil
}

func (d *Driver) getRemoteKeyWithSameFingerprintNullable(publicKeyBytes []byte) (*hcloud.SSHKey, error) {
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse ssh public key: %w", err)
	}

	fp := ssh.FingerprintLegacyMD5(publicKey)

	remoteKey, _, err := d.getClient().SSHKey.GetByFingerprint(context.Background(), fp)
	if err != nil {
		return remoteKey, fmt.Errorf("could not get sshkey by fingerprint: %w", err)
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
		return nil, fmt.Errorf("could not get client by ID: %w", err)
	}

	d.cachedServer = srv
	return srv, nil
}

func (d *Driver) waitForAction(a *hcloud.Action) error {
	progress, done := d.getClient().Action.WatchProgress(context.Background(), a)

	running := true
	var ret error

	for running {
		select {
		case <-done:
			ret = <-done
			running = false
		case <-progress:
			log.Debugf(" -> %s[%d]: %d %%", a.Command, a.ID, <-progress)
		}
	}

	if ret == nil {
		log.Debugf(" -> finished %s[%d]", a.Command, a.ID)
	}

	return ret
}

func (d *Driver) waitForMultipleActions(step string, a []*hcloud.Action) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress, watchErr := d.getClient().Action.WatchOverallProgress(ctx, a)

	running := true
	var ret error

	for running {
		select {
		case <-watchErr:
			ret = errors.Join(ret, <-watchErr)
			cancel()
		case <-progress:
			log.Debugf(" -> %s: %d %%", step, <-progress)
		default:
			running = false
		}
	}

	if ret == nil {
		log.Debugf(" -> finished %s", step)
	}

	return ret
}
