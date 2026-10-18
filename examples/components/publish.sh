#!/usr/bin/env bash
# Builds, signs and pushes both example versions to ghcr.io/chrisschneider.
# Needs: OCM CLI v2 (0.19), ghcr.io credentials (write:packages) in your .ocmconfig and
# a browser for the Sigstore keyless OIDC login (signer config in sigstore.ocmconfig).
set -euo pipefail
cd "$(dirname "$0")"
REPO=${REPO:-ghcr.io/chrisschneider}
NAME=github.com/chrisschneider/example-with-opencode-images
# The source entry points at this commit of the hub repository; push it first.
SOURCE_COMMIT=${SOURCE_COMMIT:-$(git rev-parse HEAD)}
export SOURCE_COMMIT
for spec in 1.0.0:1.33 1.1.0:1.34; do
  VERSION=${spec%%:*} K8S_MAX=${spec##*:}
  export VERSION K8S_MAX
  ocm add cv --repository "$REPO" --constructor component-constructor.yaml --component-version-conflict-policy replace
  ocm sign cv "$REPO//$NAME:$VERSION" --force --config sigstore.ocmconfig 
done
