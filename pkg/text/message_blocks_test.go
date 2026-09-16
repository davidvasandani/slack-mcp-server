package text

import (
	"encoding/json"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnitMessageRichBlocksKeepContent(t *testing.T) {
	var blocks slack.Blocks
	require.NoError(t, json.Unmarshal([]byte(`[
  {"type":"header","text":{"type":"plain_text","text":"Details"}},
  {"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
   {"type":"text","text":"  owner ","style":{"bold":true}},
   {"type":"user","user_id":"U123"},{"type":"channel","channel_id":"C123"},
   {"type":"usergroup","usergroup_id":"S123"},{"type":"emoji","name":"wave","skin_tone":3},
   {"type":"link","url":"https://example.com/details","text":"open"}
  ]},{"type":"rich_text_preformatted","elements":[{"type":"text","text":"{\n  \"a\": 1\n}"}]}]}
 ]`), &blocks))
	body := MessageBlocksToText(blocks)
	for _, value := range []string{"Details", "*  owner *", "<@U123>", "<#C123>", "<!subteam^S123>", ":wave::skin-tone-3:", "<https://example.com/details|open>", "```\n{\n  \"a\": 1\n}\n```"} {
		require.Contains(t, body, value)
	}
}

func TestUnitMessageTextPreservesFormattingAndFiltersControls(t *testing.T) {
	require.Equal(t, "  café 👩‍💻\n\t{  x  }\r\n", MessageText("\x00  café 👩‍💻\n\t{  x  }\u202e\r\n"))
}

func TestUnitMessageBlockBoundaries(t *testing.T) {
	blocks := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "1. first", false, false), nil, nil),
		slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "2. second", false, false), nil, nil),
		slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "```json\n{\n  \"a\": 1\n}\n```", false, false), nil, nil),
		slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "Next step", false, false), nil, nil),
	}}
	require.Equal(t, "1. first\n2. second\n```json\n{\n  \"a\": 1\n}\n```\nNext step", MessageBlocksToText(blocks))
}
