---
name: slack-blockkit-ui
description: Design and validate Slack Block Kit JSON for messages, modals, and App Home surfaces, including accessible fallback text and interactive elements. Use when an agent needs to create rich Slack UI or pass a `blocks` payload to `conversations_add_message`.
---

# Slack Block Kit UI

Build valid Slack Block Kit JSON using Slack's current official reference.

## Workflow

1. Identify the target surface: message, modal, or App Home.
2. Read `references/block-kit-reference.md`, then read the linked schema resource
   for every block, element, and composition object you plan to use.
3. Confirm each block is supported on the target surface and each nested element
   is compatible with its parent block.
4. Construct JSON with a top-level `blocks` array. For
   `conversations_add_message`, pass the array as the `blocks` string argument.
5. Provide concise top-level `text` containing all essential information for
   notifications and screen readers.
6. Validate required fields, maximum lengths, element counts, unique
   `action_id` values, and stable-but-renewed `block_id` values before sending.
7. Prefer a plain-text fallback if a new or restricted block is unavailable in
   the destination workspace or SDK.

## Message contract

- Send JSON, not YAML or framework-specific Ruby/Python objects.
- Treat Slack's official schema as authoritative when it conflicts with examples
  or remembered limits.
- Use `plain_text` where required by interactive controls; use `mrkdwn` only in
  fields whose schema allows it.
- Escape `&`, `<`, and `>` when placing untrusted text in Slack text objects.
- Use HTTPS image URLs and meaningful `alt_text`.
- Keep action values opaque and small; store identifiers rather than secrets or
  serialized application state.
- Never invent support for a block on a surface not listed by Slack.

## MCP usage

Call `conversations_add_message` with:

- `channel_id`: destination channel, DM, or group DM;
- `thread_ts`: optional parent message timestamp;
- `text`: accessible notification fallback;
- `blocks`: a JSON-encoded Block Kit array.

The `blocks` argument takes precedence over Markdown conversion.

## References

- `references/block-kit-reference.md`: generated index of current official
  blocks, elements, composition objects, and their schema resources.
- `references/patterns.md`: concise JSON patterns and interaction guidance.
