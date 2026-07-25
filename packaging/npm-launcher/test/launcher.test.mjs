import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { after, describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const launcher = require('../lib/launcher.js');

const here = path.dirname(fileURLToPath(import.meta.url));
const BIN = path.join(here, '..', 'bin', 'slack-mcp-server.js');

const tmpDirs = [];
function tmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'slack-mcp-vk-test-'));
  tmpDirs.push(dir);
  return dir;
}
after(() => {
  for (const dir of tmpDirs) fs.rmSync(dir, { recursive: true, force: true });
});

function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex');
}

function manifestFor(dir, payload) {
  return {
    name: 'slack-mcp-server-vk',
    version: '9.9.9-test.1',
    tag: 'v9.9.9-test.1',
    baseUrl: `file://${dir}`,
    assets: {
      'linux-x64': {
        file: 'slack-mcp-server-linux-amd64',
        sha256: sha256(payload),
        size: payload.length,
      },
    },
  };
}

function runBin(args, env, stdin = '') {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [BIN, ...args], {
      env: { ...process.env, ...env },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => (stdout += chunk));
    child.stderr.on('data', (chunk) => (stderr += chunk));
    child.on('error', reject);
    child.on('close', (code) => resolve({ code, stdout, stderr }));
    child.stdin.end(stdin);
  });
}

describe('asset resolution', () => {
  const manifest = manifestFor('/nowhere', Buffer.from('x'));

  it('resolves a supported platform', () => {
    assert.equal(
      launcher.resolveAsset(manifest, 'linux-x64').file,
      'slack-mcp-server-linux-amd64'
    );
  });

  it('rejects an unsupported platform by name', () => {
    assert.throws(
      () => launcher.resolveAsset(manifest, 'sunos-mips'),
      /unsupported platform: sunos-mips/
    );
  });

  it('rejects an asset with no recorded checksum', () => {
    const broken = { assets: { 'linux-x64': { file: 'x' } } };
    assert.throws(
      () => launcher.resolveAsset(broken, 'linux-x64'),
      /no recorded checksum for x/
    );
  });
});

describe('cache location', () => {
  it('prefers the explicit override', () => {
    assert.equal(
      launcher.cacheRoot({ SLACK_MCP_SERVER_VK_CACHE_DIR: '/tmp/pin' }, 'linux'),
      '/tmp/pin'
    );
  });

  it('uses LOCALAPPDATA on Windows and XDG_CACHE_HOME elsewhere', () => {
    assert.equal(launcher.cacheRoot({ LOCALAPPDATA: 'C:\\x' }, 'win32'), 'C:\\x');
    assert.equal(launcher.cacheRoot({ XDG_CACHE_HOME: '/c' }, 'linux'), '/c');
  });

  it('scopes the binary path by package name and version', () => {
    const manifest = manifestFor('/nowhere', Buffer.from('x'));
    assert.equal(
      launcher.binaryPath(manifest, 'linux-x64', {
        SLACK_MCP_SERVER_VK_CACHE_DIR: '/c',
      }),
      '/c/slack-mcp-server-vk/9.9.9-test.1/slack-mcp-server-linux-amd64'
    );
  });
});

describe('digest enforcement', () => {
  it('accepts a cached binary whose digest matches', async () => {
    const cache = tmpDir();
    const payload = Buffer.from('#!/bin/sh\nexit 0\n');
    const manifest = manifestFor('/nowhere', payload);
    const target = launcher.binaryPath(manifest, 'linux-x64', {
      SLACK_MCP_SERVER_VK_CACHE_DIR: cache,
    });
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, payload, { mode: 0o755 });

    const resolved = await launcher.ensureBinary({
      manifest,
      env: { SLACK_MCP_SERVER_VK_CACHE_DIR: cache },
      platform: 'linux',
      arch: 'x64',
    });
    assert.equal(resolved, target);
  });

  it('refuses a downloaded binary whose digest does not match', async () => {
    const cache = tmpDir();
    const manifest = manifestFor('/nowhere', Buffer.from('real payload'));
    const asset = manifest.assets['linux-x64'];
    const target = path.join(cache, asset.file);
    const serve = (bytes) => async (_url, destination) =>
      fs.writeFileSync(destination, bytes);

    await assert.rejects(
      launcher.fetchAndVerify(manifest, asset, target, {
        fetch: serve(Buffer.from('tampered payload')),
      }),
      /checksum mismatch for slack-mcp-server-linux-amd64: expected/
    );
    // Nothing executable is left behind — not even the staged download.
    assert.deepEqual(fs.readdirSync(cache), []);
  });

  it('installs and marks executable a download whose digest matches', async () => {
    const cache = tmpDir();
    const payload = Buffer.from('real payload');
    const manifest = manifestFor('/nowhere', payload);
    const asset = manifest.assets['linux-x64'];
    const target = path.join(cache, asset.file);

    const resolved = await launcher.fetchAndVerify(manifest, asset, target, {
      fetch: async (url, destination) => {
        assert.equal(url, `${manifest.baseUrl}/${asset.file}`);
        fs.writeFileSync(destination, payload);
      },
    });

    assert.equal(resolved, target);
    assert.deepEqual(fs.readFileSync(target), payload);
    assert.equal(fs.statSync(target).mode & 0o111, 0o111);
  });

  it('replaces a cached binary that no longer matches its digest', async () => {
    const cache = tmpDir();
    const payload = Buffer.from('real payload');
    const manifest = manifestFor('/nowhere', payload);
    const env = { SLACK_MCP_SERVER_VK_CACHE_DIR: cache };
    const target = launcher.binaryPath(manifest, 'linux-x64', env);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, Buffer.from('stale'), { mode: 0o755 });

    const warnings = [];
    const resolved = await launcher.ensureBinary({
      manifest,
      env,
      platform: 'linux',
      arch: 'x64',
      warn: (message) => warnings.push(message),
      fetch: async (_url, destination) => fs.writeFileSync(destination, payload),
    });

    assert.equal(resolved, target);
    assert.deepEqual(fs.readFileSync(target), payload);
    assert.match(warnings.join('\n'), /failed verification, re-downloading/);
  });
});

describe('binary override and process contract', () => {
  const fakeServer = (dir) => {
    const file = path.join(dir, 'fake-slack-mcp-server');
    fs.writeFileSync(
      file,
      '#!/bin/sh\necho "args:$*"\nread line\necho "stdin:$line"\nexit 42\n',
      { mode: 0o755 }
    );
    return file;
  };

  it('forwards argv and stdin and mirrors the exit code', async (t) => {
    if (process.platform === 'win32') t.skip('POSIX shell script');
    const dir = tmpDir();
    const result = await runBin(
      ['--transport', 'stdio'],
      { SLACK_MCP_SERVER_VK_BINARY: fakeServer(dir) },
      '{"jsonrpc":"2.0"}\n'
    );
    assert.equal(result.code, 42);
    assert.match(result.stdout, /args:--transport stdio/);
    assert.match(result.stdout, /stdin:\{"jsonrpc":"2\.0"\}/);
    // Nothing the launcher itself emits may land on stdout.
    assert.equal(result.stderr, '');
  });

  it('fails loudly when the override binary is not executable', async () => {
    const result = await runBin([], {
      SLACK_MCP_SERVER_VK_BINARY: '/definitely/not/here',
    });
    assert.equal(result.code, 1);
    assert.match(result.stderr, /is not executable/);
    assert.equal(result.stdout, '');
  });
});
