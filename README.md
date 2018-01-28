---
description: Hetzner Cloud driver for machine
keywords: machine, hetzner, Hetzner Cloud, driver
title: Hetzner Cloud
---

Create Docker machines on [Hetzner Cloud](https://docs.hetzner.cloud/).

You need to create a project-sepcific access token under `Access` > `API Tokens` in the project control panel
and pass that to `docker-machine create` with the `--hetzner-api-token` option.

## Building

Use an up-to-date version of [go](https://golang.org/dl)

### For use

To use the driver, you can download the sources and build it locally:

```
# Tell GO where to find sources and where to put binaries (see also https://github.com/golang/go/wiki/SettingGOPATH)
export GOPATH="$(pwd)"
export GOBIN=$GOPATH/bin
# Build the binary
go get github.com/jonasprogrammer/docker-machine-driver-hetzner
# Make the binary accessible to docker-machine
export PATH="$PATH:$GOBIN"
# Make docker-machine output help including hetzner-specific options
docker-machine create --driver hetzner
```

### For development

To compile the local sources, do the following:

```
# Check out sources
git clone https://github.com/JonasProgrammer/docker-machine-driver-hetzner.git
# Tell GO where to find sources and where to put binaries (see also https://github.com/golang/go/wiki/SettingGOPATH)
export GOPATH="$(pwd)"
export GOBIN=$GOPATH/bin
# Build the binary
go get
# Make the binary accessible to docker-machine
export PATH="$PATH:$GOBIN"
# Make docker-machine output help including hetzner-specific options
docker-machine create --driver hetzner
```
TODO: restructure code
TODO: Go version
TODO: set $GOPATH

## Usage

    $ docker-machine create --driver hetzner --hetzner-api-token=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy some-machine
    
### Using environment variables

    $ HETZNER_API_TOKEN=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy HETZNER_IMAGE=centos-7 docker-machine create --driver hetzner some-machine
    

## Options

-   `--hetzner-api-token`: **required**. Your project-specific access token for the Hetzner CLoud API.
-   `--hetzner-image`: The name of the Hetzner Cloud image to use, see [Images API](https://docs.hetzner.cloud/#resources-images-get) for how to get a list.
-   `--hetzner-server-type`: The type of the Hetzner Cloud server, see [Server Types API](https://docs.hetzner.cloud/#resources-server-types-get) for how to get a list.
-   `--hetzner-server-location`: The location to create the server in, see [Locations API](https://docs.hetzner.cloud/#resources-locations-get) for how to get a list.
**NOTICE: Beware that Hetzner does not reject invalid location names at the time of writing this; instead, a seemingly random location is chosen. Double check both the option value's
spelling and the newly created server to make sure the desired location was chosen indeed.**
-   `--hetzner-existing-key-path`: Use an existing (local) SSH key instead of generating a new keypair.
-   `--hetzner-existing-key-id`: **requires `--hetzner-existing-key-path`**. Use an existing (remote) SSH key instead of uploading the imported key pair,
    see [SSH Keys API](https://docs.hetzner.cloud/#resources-ssh-keys-get) for how to get a list

The Hetzner Cloud driver will use `debian-9` as the default image and `cx11` as the default type.

#### Existing SSH keys

When you specify the `--hetzner-existing-key-path` option, the driver will attempt to copy `(specified file name)`
and `(specified file name).pub` to the machine's store path. They public key file's permissions will be set according
to your current `umask` and the private key file will have `600` permissions.

When you additionally specify the `--hetzner-existing-key-id` option, the driver will not create an SSH key using the API
but rather try to use the existing public key corresponding to the given id. Please note that during machine creation,
the driver will attempt to [get the key](https://docs.hetzner.cloud/#resources-ssh-keys-get-1) and **compare it's
fingerprint to the local public key's fingerprtint**. Keep in mind that the both the local and the remote key must be
accessible and have matching fingerprints, otherwise the machine will fail it's pre-creation checks.

Also note that the driver will attempt to delete the linked key during machine removal, unless `--hetzner-existing-key-id`
was used during creation.

#### Environment variables and default values

| CLI option                          | Environment variable              | Default                    |
| ----------------------------------- | --------------------------------- | -------------------------- |
| **`--hetzner-api-token`**           | `HETZNER_API_TOKEN`               | -                          |
| `--hetzner-image `                  | `HETZNER_IMAGE_IMAGE`             | `debian-9`                 |
| `--hetzner-server-type`             | `HETZNER_TYPE`                    | `cx11`                     |
| `--hetzner-server-location`         | `HETZNER_LOCATION`                | - *(let Hetzner choose)*   |
| `--hetzner-existing-key-path`       | `HETZNER_EXISTING_KEY_PATH`       | - *(generate new keypair)* |
| `--hetzner-existing-key-id`         | `HETZNER_EXISTING_KEY_ID`         | 0 *(upload new key)*       |
