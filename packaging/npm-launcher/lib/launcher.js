'use strict';

// Resolution logic for the pinned slack-mcp-server build. Kept free of process
// exit / argv handling so it can be unit tested; bin/slack-mcp-server.js is the
// thin entry point that turns these errors into diagnostics and exit codes.

const crypto = require('node:crypto');
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const https = require('node:https');
const os = require('node:os');
const path = require('node:path');

const PACKAGE_DIR_NAME = 'slack-mcp-server-vk';
const MAX_REDIRECTS = 5;

class LauncherError extends Error {}

function assetKey(platform = process.platform, arch = process.arch) {
  return `${platform}-${arch}`;
}

function resolveAsset(manifest, key) {
  const asset = manifest.assets && manifest.assets[key];
  if (!asset) {
    throw new LauncherError(`unsupported platform: ${key}`);
  }
  if (!asset.sha256) {
    throw new LauncherError(`no recorded checksum for ${asset.file}`);
  }
  return asset;
}

function cacheRoot(env = process.env, platform = process.platform) {
  if (env.SLACK_MCP_SERVER_VK_CACHE_DIR) {
    return env.SLACK_MCP_SERVER_VK_CACHE_DIR;
  }
  if (platform === 'win32' && env.LOCALAPPDATA) {
    return env.LOCALAPPDATA;
  }
  if (env.XDG_CACHE_HOME) {
    return env.XDG_CACHE_HOME;
  }
  return path.join(os.homedir(), '.cache');
}

// Version-scoped, so a new pin never collides with a cached older build and a
// rollback re-uses the previous entry instead of re-downloading it.
function binaryPath(manifest, key, env, platform) {
  const asset = resolveAsset(manifest, key);
  return path.join(
    cacheRoot(env, platform),
    PACKAGE_DIR_NAME,
    manifest.version,
    asset.file
  );
}

function sha256File(file) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    const stream = fs.createReadStream(file);
    stream.on('error', reject);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolve(hash.digest('hex')));
  });
}

function download(url, destination, redirectsLeft = MAX_REDIRECTS) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, (response) => {
      const status = response.statusCode;
      const location = response.headers.location;

      if (status >= 300 && status < 400 && location) {
        response.resume();
        if (redirectsLeft === 0) {
          reject(new LauncherError('too many redirects'));
          return;
        }
        const next = new URL(location, url).toString();
        download(next, destination, redirectsLeft - 1).then(resolve, reject);
        return;
      }

      if (status !== 200) {
        response.resume();
        reject(new LauncherError(`HTTP status ${status}`));
        return;
      }

      const file = fs.createWriteStream(destination);
      response.pipe(file);
      file.on('error', reject);
      file.on('finish', () => file.close((err) => (err ? reject(err) : resolve())));
    });
    request.on('error', (err) => reject(new LauncherError(err.message)));
  });
}

// `fetch` is injectable so tests can exercise verification without a network
// (and without teaching the production path any protocol but https).
async function fetchAndVerify(manifest, asset, target, { fetch = download } = {}) {
  const url = `${manifest.baseUrl}/${asset.file}`;
  await fsp.mkdir(path.dirname(target), { recursive: true });

  // Download beside the target and rename only after the digest matches, so an
  // interrupted or tampered download is never executable.
  const staged = `${target}.${process.pid}.${Date.now()}.tmp`;
  try {
    try {
      await fetch(url, staged);
    } catch (err) {
      throw new LauncherError(`download failed for ${asset.file}: ${err.message}`);
    }

    const actual = await sha256File(staged);
    if (actual !== asset.sha256) {
      throw new LauncherError(
        `checksum mismatch for ${asset.file}: expected ${asset.sha256}, got ${actual}`
      );
    }

    await fsp.chmod(staged, 0o755);
    await fsp.rename(staged, target);
  } finally {
    await fsp.rm(staged, { force: true });
  }

  return target;
}

// Returns the path of a binary that is verified to match the pinned digest, or
// the operator-supplied binary when SLACK_MCP_SERVER_VK_BINARY is set.
async function ensureBinary({
  manifest,
  env = process.env,
  platform = process.platform,
  arch = process.arch,
  warn = () => {},
  fetch = download,
} = {}) {
  const override = env.SLACK_MCP_SERVER_VK_BINARY;
  if (override) {
    try {
      fs.accessSync(override, fs.constants.X_OK);
    } catch {
      throw new LauncherError(
        `SLACK_MCP_SERVER_VK_BINARY=${override} is not executable`
      );
    }
    return override;
  }

  const key = assetKey(platform, arch);
  const asset = resolveAsset(manifest, key);
  const target = binaryPath(manifest, key, env, platform);

  if (fs.existsSync(target)) {
    const cached = await sha256File(target);
    if (cached === asset.sha256) {
      return target;
    }
    warn(`cached ${asset.file} failed verification, re-downloading`);
    await fsp.rm(target, { force: true });
  }

  return fetchAndVerify(manifest, asset, target, { fetch });
}

function loadManifest(file) {
  let raw;
  try {
    raw = fs.readFileSync(file, 'utf8');
  } catch {
    throw new LauncherError(
      `missing ${path.basename(file)} — this package must be built by scripts/build-release.sh, not run from a source checkout`
    );
  }
  return JSON.parse(raw);
}

module.exports = {
  LauncherError,
  assetKey,
  binaryPath,
  cacheRoot,
  ensureBinary,
  fetchAndVerify,
  loadManifest,
  resolveAsset,
  sha256File,
};
