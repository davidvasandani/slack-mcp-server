#!/usr/bin/env bash
# Build every release artifact for a fork release tag:
#   dist/slack-mcp-server-<os>-<arch>[.exe]   the Go binaries
#   dist/slack-mcp-server-vk-<version>.tgz    the npm launcher (digests baked in)
#   dist/checksums.txt                        sha256 of everything above
#
# Run it from a checked-out tag; the tag is stamped into the binaries by the
# Makefile's `git describe --tags` ldflags and into the launcher manifest.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

launcher_dir="packaging/npm-launcher"
dist_dir="dist"

version="$(node -p "require('./$launcher_dir/package.json').version")"
tag="${RELEASE_TAG:-$(git describe --tags --exact-match 2>/dev/null || true)}"
if [ -z "$tag" ]; then
  echo "build-release: HEAD is not tagged; set RELEASE_TAG=vX.Y.Z-vk.N to override" >&2
  exit 1
fi
if [ "$tag" != "v$version" ]; then
  echo "build-release: tag $tag does not match launcher version $version" >&2
  echo "               update $launcher_dir/package.json first" >&2
  exit 1
fi

base_url="https://github.com/davidvasandani/slack-mcp-server/releases/download/$tag"

# Static binaries: no cgo, so a downloaded binary does not depend on the host's
# libc, and cross-compilation needs no toolchain per target.
export CGO_ENABLED=0

# Same ldflags as `make build-all-platforms`, with two changes that make a
# rebuild of the same tag byte-identical (the digests we publish are the pin):
# BuildTime comes from the tagged commit instead of the wall clock, and
# -trimpath removes the builder's directory layout.
package="$(go list -m)"
commit_hash="$(git rev-parse HEAD)"
# TZ is pinned: `--date=format-local` renders in the *builder's* timezone, so
# without this the same tag stamps a different BuildTime — and a different
# digest — on a machine that is not on UTC.
build_time="$(TZ=UTC0 git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ HEAD)"
binary_name="slack-mcp-server"
ld_flags="-s -w \
  -X '$package/pkg/version.CommitHash=$commit_hash' \
  -X '$package/pkg/version.Version=$tag' \
  -X '$package/pkg/version.BuildTime=$build_time' \
  -X '$package/pkg/version.BinaryName=$binary_name'"

rm -rf "$dist_dir"
mkdir -p "$dist_dir"

echo "build-release: building binaries for $tag"
for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  suffix=""
  [ "$goos" = "windows" ] && suffix=".exe"
  echo "  $goos/$goarch"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ld_flags" \
    -o "$dist_dir/$binary_name-$goos-$goarch$suffix" ./cmd/slack-mcp-server
done

echo "build-release: writing $launcher_dir/checksums.json"
node - "$dist_dir" "$launcher_dir/checksums.json" "$version" "$tag" "$base_url" <<'NODE'
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const [distDir, out, version, tag, baseUrl] = process.argv.slice(2);

// Node's platform-arch identifiers → the Makefile's GOOS-GOARCH artifact names.
const targets = {
  'darwin-arm64': 'slack-mcp-server-darwin-arm64',
  'darwin-x64': 'slack-mcp-server-darwin-amd64',
  'linux-arm64': 'slack-mcp-server-linux-arm64',
  'linux-x64': 'slack-mcp-server-linux-amd64',
  'win32-arm64': 'slack-mcp-server-windows-arm64.exe',
  'win32-x64': 'slack-mcp-server-windows-amd64.exe',
};

const assets = {};
for (const [key, file] of Object.entries(targets)) {
  const full = path.join(distDir, file);
  const bytes = fs.readFileSync(full);
  assets[key] = {
    file,
    sha256: crypto.createHash('sha256').update(bytes).digest('hex'),
    size: bytes.length,
  };
}

fs.writeFileSync(
  out,
  `${JSON.stringify({ name: 'slack-mcp-server-vk', version, tag, baseUrl, assets }, null, 2)}\n`
);
NODE

echo "build-release: testing the launcher"
(cd "$launcher_dir" && npm test)

echo "build-release: packing the launcher"
(cd "$launcher_dir" && npm pack --pack-destination "$repo_root/$dist_dir" >/dev/null)

expected_tarball="$dist_dir/slack-mcp-server-vk-$version.tgz"
if [ ! -f "$expected_tarball" ]; then
  echo "build-release: expected $expected_tarball; npm pack produced:" >&2
  ls "$dist_dir"/*.tgz >&2
  exit 1
fi

echo "build-release: writing $dist_dir/checksums.txt"
(cd "$dist_dir" && sha256sum slack-mcp-server-* > checksums.txt)

echo
echo "build-release: artifacts for $tag"
cat "$dist_dir/checksums.txt"
