# Installation

Install this fork from its `nisedo-dev` branch. Homebrew, `github.com/crytic/medusa`, and
`ghcr.io/crytic/medusa` install the upstream version and do not include this fork's changes.

### Prerequisites

- [Go 1.26.10 or later](https://go.dev/doc/install)
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
