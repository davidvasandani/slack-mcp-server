# Block Kit patterns

## Accessible message

```json
{
  "text": "Deployment api-42 succeeded in production.",
  "blocks": [
    {
      "type": "header",
      "text": { "type": "plain_text", "text": "Deployment succeeded" }
    },
    {
      "type": "section",
      "fields": [
        { "type": "mrkdwn", "text": "*Service:*\napi" },
        { "type": "mrkdwn", "text": "*Version:*\n42" }
      ]
    }
  ]
}
```

The top-level `text` is the notification and screen-reader fallback. Pass only
the value of `blocks` to the MCP tool's `blocks` argument.

## Interactive action

```json
{
  "type": "actions",
  "block_id": "deployment-actions-42",
  "elements": [
    {
      "type": "button",
      "text": { "type": "plain_text", "text": "View deployment" },
      "action_id": "deployment.view",
      "value": "42",
      "url": "https://example.com/deployments/42"
    }
  ]
}
```

Use unique `action_id` values within a message. Acknowledge interactive payloads
quickly and perform slow work asynchronously. Renew `block_id` when updating a
message or view.

## Formatting

Slack `mrkdwn` uses `*bold*`, `_italic_`, `~strike~`, and `<URL|label>` links.
Do not assume CommonMark syntax is accepted. Consult the current text-object and
formatting references for field-specific rules.
