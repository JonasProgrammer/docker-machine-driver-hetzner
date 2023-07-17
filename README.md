# Hetzner Cloud Docker machine driver

[![Go Report Card](https://goreportcard.com/badge/github.com/JonasProgrammer/docker-machine-driver-hetzner)](https://goreportcard.com/report/github.com/JonasProgrammer/docker-machine-driver-hetzner)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go-CI](https://github.com/JonasProgrammer/docker-machine-driver-hetzner/actions/workflows/go.yml/badge.svg)](https://github.com/JonasProgrammer/docker-machine-driver-hetzner/actions/workflows/go.yml)

> This library adds the support for creating [Docker machines](https://github.com/docker/machine) hosted on the [Hetzner Cloud](https://www.hetzner.de/cloud).

You need to create a project-specific access token under `Access` > `API Tokens` in the project control panel
and pass that to `docker-machine create` with the `--hetzner-api-token` option.

## Installation

You can find sources and pre-compiled binaries [here](https://github.com/JonasProgrammer/docker-machine-driver-hetzner/releases).

```bash
# Download the binary (this example downloads the binary for linux amd64)
$ wget https://github.com/JonasProgrammer/docker-machine-driver-hetzner/releases/download/4.1.0/docker-machine-driver-hetzner_4.1.0_linux_amd64.tar.gz
$ tar -xvf docker-machine-driver-hetzner_4.1.0_linux_amd64.tar.gz

# Make it executable and copy the binary in a directory accessible with your $PATH
$ chmod +x docker-machine-driver-hetzner
$ cp docker-machine-driver-hetzner /usr/local/bin/
```

## Usage

```bash
$ docker-machine create \
  --driver hetzner \
  --hetzner-api-token=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy \
  some-machine
```

### Using environment variables

```bash
$ HETZNER_API_TOKEN=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy \
  && HETZNER_IMAGE=centos-7 \
  && docker-machine create \
     --driver hetzner \
     some-machine
```

### Dealing with kernels without aufs

If you use an image without aufs, like the one currently supplied with the
debian-9 image, you can try specifying another storage driver, such as
overlay2. Like so:

```bash
$ docker-machine create \
  --engine-storage-driver overlay2 \
  --driver hetzner \
  --hetzner-image debian-9 \
  --hetzner-api-token=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy \
  some-machine
```

### Using Cloud-init

```bash
$ CLOUD_INIT_USER_DATA=`cat <<EOF
#cloud-config
write_files:
  - path: /test.txt
    content: |
      Here is a line.
      Another line is here.
EOF
`

$ docker-machine create \
  --driver hetzner \
  --hetzner-api-token=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy \
  --hetzner-user-data="${CLOUD_INIT_USER_DATA}" \
  some-machine
```

### Using a snapshot

Assuming your snapshot ID is `424242`:
```bash
$ docker-machine create \
  --driver hetzner \
  --hetzner-api-token=QJhoRT38JfAUO037PWJ5Zt9iAABIxdxdh4gPqNkUGKIrUMd6I3cPIsfKozI513sy \
  --hetzner-image-id=424242 \
  some-machine
```

## Options

- `--hetzner-api-token`: **required**. Your project-specific access token for the Hetzner Cloud API.
- `--hetzner-image`: The name (or ID) of the Hetzner Cloud image to use, see [Images API](https://docs.hetzner.cloud/#images-get-all-images) for how to get a list (currently defaults to `ubuntu-20.04`). *Explicitly specifying an image is **strongly** recommended and will be **required from v6 onwards***.
- `--hetzner-image-arch`: The architecture to use during image lookup, inferred from the server type if not explicitly given.
- `--hetzner-image-id`: The id of the Hetzner cloud image (or snapshot) to use, see [Images API](https://docs.hetzner.cloud/#images-get-all-images) for how to get a list (mutually excludes `--hetzner-image`).
- `--hetzner-server-type`: The type of the Hetzner Cloud server, see [Server Types API](hhttps://docs.hetzner.cloud/#server-types-get-all-server-types) for how to get a list (defaults to `cx11`).
- `--hetzner-server-location`: The location to create the server in, see [Locations API](https://docs.hetzner.cloud/#locations-get-all-locations) for how to get a list.
- `--hetzner-existing-key-path`: Use an existing (local) SSH key instead of generating a new keypair. If a remote key with a matching fingerprint exists, it will be used as if specified using `--hetzner-existing-key-id`, rather than uploading a new key.
- `--hetzner-existing-key-id`: **requires `--hetzner-existing-key-path`**. Use an existing (remote) SSH key instead of uploading the imported key pair,
  see [SSH Keys API](https://docs.hetzner.cloud/#ssh-keys-get-all-ssh-keys) for how to get a list
- `--hetzner-additional-key`: Upload an additional public key associated with the server, or associate an existing one with the same fingerprint. Can be specified multiple times.
- `--hetzner-user-data`: Cloud-init based data, passed inline as-is.
- `--hetzner-user-data-file`: Cloud-init based data, read from passed file.
- `--hetzner-user-data-from-file`: DEPRECATED, use `--hetzner-user-data-file`. Read `--hetzner-user-data` as file name and use contents as user-data.
- `--hetzner-volumes`: Volume IDs or names which should be attached to the server
- `--hetzner-networks`: Network IDs or names which should be attached to the server private network interface
- `--hetzner-use-private-network`: Use private network
- `--hetzner-firewalls`: Firewall IDs or names which should be applied on the server
- `--hetzner-server-label`: `key=value` pairs of additional metadata to assign to the server.
- `--hetzner-key-label`: `key=value` pairs of additional metadata to assign to SSH key (only applies if newly created).
- `--hetzner-placement-group`: Add to a placement group by name or ID; a spread-group will be created on demand if it does not exist
- `--hetzner-auto-spread`: Add to a `docker-machine` provided `spread` group (mutually exclusive with `--hetzner-placement-group`)
- `--hetzner-ssh-user`: Change the default SSH-User
- `--hetzner-ssh-port`: Change the default SSH-Port
- `--hetzner-primary-ipv4/6`: Sets an existing primary IP (v4 or v6 respectively) for the server, as documented in [Networking](#networking)
- `--hetzner-wait-on-error`: Amount of seconds to wait on server creation failure (0/no wait by default)
- `--hetzner-wait-on-polling`: Amount of seconds to wait between requests when waiting for some state to change. (Default: 1 second)
- `--hetzner-wait-for-running-timeout`: Max amount of seconds to wait until a machine is running. (Default: 0/no timeout)

Please beware, that for options referring to entities by name, such as server locations and types, the names used by the API may differ from the ones
shown in the server creation UI. If server creation fails due to a failure to resolve such issues, try another variant of the name (e.g. lowercase,
kebab-case). As of writing, server types use lowercase (i.e. `cx21` instead of `CX21`) and locations use a three-letter abbreviation suffixed by 1
(i.e. `fsn1` instead of `Falkenstein`).

#### Image selection

When `--hetzner-image-id` is passed, it will be used for lookup by ID as-is. No additional validation is performed, and it is mutually exclusive with
other `--hetzner-image*`-flags.

When `--hetzner-image` is passed, lookup will happen either by name or by ID as per Hetzner-supplied logic. The lookup mechanism will filter by image
architecture, which is usually inferred from the server type. One may explicitly specify it using `--hetzner-image-arch` in which case the user
supplied value will take precedence.

While there is currently a default image as fallback, this behaviour will be removed in a future version. Explicitly specifying an operating system
image is strongly recommended for new deployments, and will be mandatory in upcoming versions.

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

| CLI option                           | Environment variable               | Default                    |
|--------------------------------------|------------------------------------|----------------------------|
| **`--hetzner-api-token`**            | `HETZNER_API_TOKEN`                |                            |
| `--hetzner-image`                    | `HETZNER_IMAGE`                    | `ubuntu-20.04` as fallback |
| `--hetzner-image-arch`               | `HETZNER_IMAGE_ARCH`               | *(infer from server)*      |
| `--hetzner-image-id`                 | `HETZNER_IMAGE_ID`                 |                            |
| `--hetzner-server-type`              | `HETZNER_TYPE`                     | `cx11`                     |
| `--hetzner-server-location`          | `HETZNER_LOCATION`                 | *(let Hetzner choose)*     |
| `--hetzner-existing-key-path`        | `HETZNER_EXISTING_KEY_PATH`        | *(generate new keypair)*   |
| `--hetzner-existing-key-id`          | `HETZNER_EXISTING_KEY_ID`          | 0 *(upload new key)*       |
| `--hetzner-additional-key`           | `HETZNER_ADDITIONAL_KEYS`          |                            |
| `--hetzner-user-data`                | `HETZNER_USER_DATA`                |                            |
| `--hetzner-user-data-file`           | `HETZNER_USER_DATA_FILE`           |                            |
| `--hetzner-networks`                 | `HETZNER_NETWORKS`                 |                            |
| `--hetzner-firewalls`                | `HETZNER_FIREWALLS`                |                            |
| `--hetzner-volumes`                  | `HETZNER_VOLUMES`                  |                            |
| `--hetzner-use-private-network`      | `HETZNER_USE_PRIVATE_NETWORK`      | false                      |
| `--hetzner-disable-public-ipv4`      | `HETZNER_DISABLE_PUBLIC_IPV4`      | false                      |
| `--hetzner-disable-public-ipv6`      | `HETZNER_DISABLE_PUBLIC_IPV6`      | false                      |
| `--hetzner-disable-public`           | `HETZNER_DISABLE_PUBLIC`           | false                      |
| `--hetzner-server-label`             | (inoperative)                      | `[]`                       |
| `--hetzner-key-label`                | (inoperative)                      | `[]`                       |
| `--hetzner-placement-group`          | `HETZNER_PLACEMENT_GROUP`          |                            |
| `--hetzner-auto-spread`              | `HETZNER_AUTO_SPREAD`              | false                      |
| `--hetzner-ssh-user`                 | `HETZNER_SSH_USER`                 | root                       |
| `--hetzner-ssh-port`                 | `HETZNER_SSH_PORT`                 | 22                         |
| `--hetzner-primary-ipv4`             | `HETZNER_PRIMARY_IPV4`             |                            |
| `--hetzner-primary-ipv6`             | `HETZNER_PRIMARY_IPV6`             |                            |
| `--hetzner-wait-on-error`            | `HETZNER_WAIT_ON_ERROR`            | 0                          |
| `--hetzner-wait-on-polling`          | `HETZNER_WAIT_ON_POLLING`          | 1                          |
| `--hetzner-wait-for-running-timeout` | `HETZNER_WAIT_FOR_RUNNING_TIMEOUT` | 0                          |

#### Networking

Given `--hetzner-primary-ipv4` or `--hetzner-primary-ipv6`, the driver
attempts to set up machine creation with an existing [primary IP](https://docs.hetzner.com/cloud/servers/primary-ips/overview/)
as follows: If the passed argument parses to a valid IP address, the primary IP is resolved via address.
Otherwise, it is resolved in the default Hetzner Cloud API way (i.e. via ID and name as a fallback).

No address family validation is performed, so when specifying an IP address it is the user's responsibility to pass the
appropriate type. This also applies to any given preconditions regarding the state of the address being attached.

If no existing primary IPs are specified and public address creation is not disabled for a given address family, a new
primary IP will be auto-generated by default. Primary IPs created in that fashion will exhibit whatever default behavior
Hetzner assigns them at the given time, so users should take care what retention flags etc. are being set.

When disabling all public IPs, `--hetzner-use-private-network` must be given.
`--hetzner-disable-public` will take care of that, and behaves as if
`--hetzner-disable-public-ipv4 --hetzner-disable-public-ipv6 --hetzner-use-private-network`
were given.
Using `--hetzner-use-private-network` implicitly or explicitly requires at least one `--hetzner-network`
to be given.

## Building from source

Use an up-to-date version of [Go](https://golang.org/dl) to use Go Modules.

To use the driver, you can download the sources and build it locally:

```shell
# Enable Go Modules if you are not outside of your $GOPATH
$ export GO111MODULE=on

# Get sources and build the binary at ~/go/bin/docker-machine-driver-hetzner
$ go get github.com/jonasprogrammer/docker-machine-driver-hetzner

# Make the binary accessible to docker-machine
$ export GOPATH=$(go env GOPATH)
$ export GOBIN=$GOPATH/bin
$ export PATH="$PATH:$GOBIN"
$ cd $GOPATH/src/jonasprogrammer/docker-machine-driver-hetzner
$ go build -o docker-machine-driver-hetzner
$ cp docker-machine-driver-hetzner /usr/local/bin/docker-machine-driver-hetzner
```

## Development

Fork this repository, yielding `github.com/<yourAccount>/docker-machine-driver-hetzner`.

```shell
# Get the sources of your fork and build it locally
$ go get github.com/<yourAccount>/docker-machine-driver-hetzner

# * This integrates your fork into the $GOPATH (typically pointing at ~/go)
# * Your sources are at $GOPATH/src/github.com/<yourAccount>/docker-machine-driver-hetzner
# * That folder is a local Git repository. You can pull, commit and push from there.
# * The binary will typically be at $GOPATH/bin/docker-machine-driver-hetzner
# * In the source directory $GOPATH/src/github.com/<yourAccount>/docker-machine-driver-hetzner
#   you may use go get to re-build the binary.
# * Note: when you build the driver from different repositories, e.g. from your fork
#   as well as github.com/jonasprogrammer/docker-machine-driver-hetzner,
#   the binary files generated by these builds are all called the same
#   and will hence override each other.

# Make the binary accessible to docker-machine
$ export GOPATH=$(go env GOPATH)
$ export GOBIN=$GOPATH/bin
$ export PATH="$PATH:$GOBIN"

# Make docker-machine output help including hetzner-specific options
$ docker-machine create --driver hetzner
```

## Upcoming breaking changes

### 4.0.0

* **check log output for BREAKING-V5**
* `--hetzner-user-data-from-file` will be fully deprecated and its flag description will only read 'DEPRECATED, legacy'; current fallback behaviour will be retained. `--hetzner-flag-user-data-file` should be used instead.
* `--hetzner-disable-public-4`/`--hetzner-disable-public-6` will be fully deprecated and its flag description will only read 'DEPRECATED, legacy'; current fallback behaviour will be retained. `--hetzner-disable-public-ipv4`/`--hetzner-disable-public-ipv6` should be used instead.

### 5.0.0

* `--hetzner-user-data-from-file` will be removed entirely, including its fallback behavior
* `--hetzner-disable-public-4`/`--hetzner-disable-public-6` ill be removed entirely, including their fallback behavior
* not specifying `--hetzner-image` will generate a warning stating 'use of default image is DEPRECATED'

### 6.0.0

* specifying `--hetzner-image` will be mandatory, and a default image will no longer be provided
