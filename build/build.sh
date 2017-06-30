#!/bin/sh

set -o errexit
set -o nounset
# set -o pipefail

if [ -z "${PKG}" ]; then
    echo "PKG must be set"
    exit 1
fi
if [ -z "${VERSION}" ]; then
    echo "VERSION must be set"
    exit 1
fi

export CGO_ENABLED=1

go install                                                         \
    -ldflags "-X ${PKG}/pkg/version.VERSION=${VERSION}"            \
    -tags libvirt.1.2.14					   \
    ./...
