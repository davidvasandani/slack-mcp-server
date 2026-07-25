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

echo "build-release: building binaries for $tag"
make build-all-platforms

rm -rf "$dist_dir"
mkdir -p "$dist_dir"
cp build/slack-mcp-server-* "$dist_dir/"

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
