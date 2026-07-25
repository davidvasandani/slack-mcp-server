# slack-mcp-server-vk

Launcher for the pinned fork build of `slack-mcp-server`. It exists so that
[Vibe Kanban](https://github.com/BloopAI/vibe-kanban)'s bundled Slack MCP
catalog entry can install **this fork** — including `attachment_get_data` — from
one immutable, digest-verified artifact:

```jsonc
"slack": {
  "command": "npx",
  "args": [
    "-y",
    "https://github.com/davidvasandani/slack-mcp-server/releases/download/v1.3.0-vk.1/slack-mcp-server-vk-1.3.0-vk.1.tgz",
    "--transport",
    "stdio"
  ],
  "env": { "SLACK_MCP_XOXP_TOKEN": "YOUR_TOKEN" }
}
```

## Why this package is not on npm

The `slack-mcp-server` name on npm belongs to the upstream project, and this
fork has no registry namespace of its own. Rather than pin a mutable `@latest`
tag that resolves to upstream code, the launcher is shipped as a GitHub release
asset and installed by URL — `npx` accepts a remote tarball as a package spec.
`private: true` is set so it can never be published by accident.

## What it does

1. `SLACK_MCP_SERVER_VK_BINARY` set → runs that binary and nothing else. The
   operator supplied it, so the operator owns its provenance (offline hosts,
   air-gapped installs, local development builds).
2. Otherwise resolves `${process.platform}-${process.arch}` to a release asset,
   looks for it in
   `<cache-root>/slack-mcp-server-vk/<version>/<asset>`, and verifies its
   SHA-256 against `checksums.json` (baked in at build time).
3. On a miss, downloads the asset from the release this package was built for,
   verifies the digest, `chmod 0755`, and renames it into the cache — a staged
   download that fails verification is deleted, never executed.
4. Executes the binary with the caller's arguments and inherited stdio, forwards
   `SIGINT`/`SIGTERM`/`SIGHUP`, and exits with the child's code (or re-raises its
   signal).

Everything on stdout belongs to the MCP stdio transport; launcher diagnostics go
to stderr only, one line each, prefixed `slack-mcp-server-vk:`. Any failure —
unsupported platform, download error, digest mismatch, missing override binary —
exits non-zero with a diagnostic. It never falls back to a different build.

### Environment

| Variable | Effect |
| --- | --- |
| `SLACK_MCP_SERVER_VK_BINARY` | Run this binary; skip download and digest check |
| `SLACK_MCP_SERVER_VK_CACHE_DIR` | Cache root override (default: `%LOCALAPPDATA%`, `$XDG_CACHE_HOME`, or `~/.cache`) |

Slack credentials (`SLACK_MCP_XOXP_TOKEN`, `SLACK_MCP_ENABLED_TOOLS`, …) are
passed through untouched to the server.

## Cutting a new release

`checksums.json` is a build output (git-ignored): it can only be written after
the binaries exist.

```bash
git tag -a v1.3.0-vk.2 -m "Release v1.3.0-vk.2"      # <base>-vk.<n>
scripts/build-release.sh                              # builds, tests, packs
gh release create v1.3.0-vk.2 dist/* --title …        # upload every asset
```

Then update the consumer: the URL in `crates/executors/default_mcp.json`, the
`SLACK_MCP_FORK_TAG` / `SLACK_MCP_LAUNCHER_SHA256` constants in
`crates/executors/src/mcp_config.rs`, and the documented version — in one
change.

Release assets are immutable. If something is wrong with a published release,
cut `-vk.<n+1>`; never re-upload an asset under an existing tag.
