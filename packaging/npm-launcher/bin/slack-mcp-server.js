#!/usr/bin/env node
'use strict';

// Entry point for the pinned fork build. Everything on stdout belongs to the
// MCP stdio transport, so this process writes diagnostics to stderr only and
// hands the real file descriptors straight to the Go binary.

const path = require('node:path');
const { spawn } = require('node:child_process');

const { ensureBinary, loadManifest } = require('../lib/launcher.js');

const PREFIX = 'slack-mcp-server-vk:';
const FORWARDED_SIGNALS = ['SIGINT', 'SIGTERM', 'SIGHUP'];

function warn(message) {
  process.stderr.write(`${PREFIX} ${message}\n`);
}

function run(binary) {
  const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });

  const forward = (signal) => () => {
    if (child.exitCode === null && child.signalCode === null) {
      child.kill(signal);
    }
  };
  const handlers = FORWARDED_SIGNALS.map((signal) => {
    const handler = forward(signal);
    process.on(signal, handler);
    return [signal, handler];
  });

  child.on('error', (err) => {
    warn(`failed to start ${binary}: ${err.message}`);
    process.exit(1);
  });

  child.on('exit', (code, signal) => {
    for (const [name, handler] of handlers) {
      process.removeListener(name, handler);
    }
    if (signal) {
      // Re-raise so the parent's death looks like the child's to whoever
      // supervises this process.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

async function main() {
  // An operator-supplied binary owns its own provenance, so the pinned manifest
  // is not read (and need not exist) in that case.
  const manifest = process.env.SLACK_MCP_SERVER_VK_BINARY
    ? null
    : loadManifest(path.join(__dirname, '..', 'checksums.json'));
  const binary = await ensureBinary({ manifest, warn });
  run(binary);
}

main().catch((err) => {
  warn(err.message);
  process.exit(1);
});
