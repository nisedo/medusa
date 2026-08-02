# Installation

Install this maintained fork from its `nisedo-dev` branch. Homebrew, `github.com/crytic/medusa`, and
`ghcr.io/crytic/medusa` install the upstream version and do not include this fork's changes.

### Prerequisites

- [Go 1.24 or later](https://go.dev/doc/install)
- [`crytic-compile`](https://github.com/crytic/crytic-compile#installation) for the default compilation platform
- [Slither](https://github.com/crytic/slither#how-to-install) for its optional value-generation analysis

## Install from a checkout

```shell
git clone --branch nisedo-dev --single-branch https://github.com/nisedo/medusa.git
cd medusa
go install .
```

Go writes the binary to `GOBIN`, or to `GOPATH/bin` when `GOBIN` is unset. Verify which binary your shell will run:

```shell
command -v medusa
medusa --version
```

Do not run `go install github.com/nisedo/medusa@nisedo-dev`. The fork retains the module path
`github.com/crytic/medusa`, so Go rejects installation through the fork URL.

## Nix

From the same checkout:

```shell
nix build
nix profile install ./result
```

The build output is also available at `./result/bin/medusa`.

## Docker

Pushes to `nisedo-dev` publish `ghcr.io/nisedo/medusa:testing-nisedo-dev`:

```shell
docker pull ghcr.io/nisedo/medusa:testing-nisedo-dev
docker run --rm -it -v "$PWD:/src" -w /src \
  ghcr.io/nisedo/medusa:testing-nisedo-dev medusa fuzz
```

The image has no entrypoint. Keep `medusa` before the subcommand, and set `/src` as the working directory so Medusa
can find the mounted project.
