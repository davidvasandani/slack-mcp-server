package handler

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestUnitRawBlockPreservesUnknownSchemaFields(t *testing.T) {
	original := json.RawMessage(`{"type":"card","block_id":"card-1","title":{"type":"plain_text","text":"Current schema"},"future_field":{"nested":true}}`)
	blocks := []slack.Block{rawBlock{
		raw:     original,
		typ:     slack.MessageBlockType("card"),
		blockID: "card-1",
	}}

	encoded, err := json.Marshal(blocks)
	require.NoError(t, err)
	require.JSONEq(t, `[`+string(original)+`]`, string(encoded))
}
