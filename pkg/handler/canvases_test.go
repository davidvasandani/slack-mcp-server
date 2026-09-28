package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/korotovsky/slack-mcp-server/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeCanvasSlackAPI struct {
	provider.SlackAPI
	editErr     error
	edits       []provider.CanvasChange
	editIDs     []string
	sections    []string
	lookupErr   error
	lookupCrits []provider.CanvasSectionCriteria
}

func (f *fakeCanvasSlackAPI) EditCanvasContext(_ context.Context, canvasID string, change provider.CanvasChange) error {
	f.editIDs = append(f.editIDs, canvasID)
	f.edits = append(f.edits, change)
	return f.editErr
}

func (f *fakeCanvasSlackAPI) LookupCanvasSectionsContext(_ context.Context, _ string, criteria provider.CanvasSectionCriteria) ([]string, error) {
	f.lookupCrits = append(f.lookupCrits, criteria)
	return f.sections, f.lookupErr
}

func canvasesHandler(api provider.SlackAPI) *CanvasesHandler {
	return &CanvasesHandler{
		slack:   api,
		isReady: func() (bool, error) { return true, nil },
		logger:  zap.NewNop(),
	}
}

func canvasRequest(args map[string]any) mcp.CallToolRequest {
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args
	return request
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func TestParseCanvasEditParams_Operations(t *testing.T) {
	markdown := func(text string) *provider.CanvasContent {
		return &provider.CanvasContent{Type: "markdown", Markdown: text}
	}
	tests := []struct {
		name string
		args map[string]any
		want provider.CanvasChange
	}{
		{
			name: "default operation appends",
			args: map[string]any{"canvas_id": " F0123ABCD ", "markdown": "## Report"},
			want: provider.CanvasChange{Operation: "insert_at_end", DocumentContent: markdown("## Report")},
		},
		{
			name: "insert at start",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "insert_at_start", "markdown": "top"},
			want: provider.CanvasChange{Operation: "insert_at_start", DocumentContent: markdown("top")},
		},
		{
			name: "insert after section",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "insert_after", "section_id": "temp:C:abc", "markdown": "x"},
			want: provider.CanvasChange{Operation: "insert_after", SectionID: "temp:C:abc", DocumentContent: markdown("x")},
		},
		{
			name: "insert before section",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "insert_before", "section_id": "temp:C:abc", "markdown": "x"},
			want: provider.CanvasChange{Operation: "insert_before", SectionID: "temp:C:abc", DocumentContent: markdown("x")},
		},
		{
			name: "replace one section",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "replace", "section_id": "temp:C:abc", "markdown": "new"},
			want: provider.CanvasChange{Operation: "replace", SectionID: "temp:C:abc", DocumentContent: markdown("new")},
		},
		{
			name: "confirmed whole-canvas replace",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "replace", "replace_entire_canvas": true, "markdown": "all"},
			want: provider.CanvasChange{Operation: "replace", DocumentContent: markdown("all")},
		},
		{
			name: "delete section",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "delete", "section_id": "temp:C:abc"},
			want: provider.CanvasChange{Operation: "delete", SectionID: "temp:C:abc"},
		},
		{
			name: "rename",
			args: map[string]any{"canvas_id": "F0123ABCD", "operation": "rename", "title": "Audit"},
			want: provider.CanvasChange{Operation: "rename", TitleContent: markdown("Audit")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := parseCanvasEditParams(canvasRequest(tt.args))
			require.NoError(t, err)
			assert.Equal(t, "F0123ABCD", params.canvasID)
			assert.Equal(t, tt.want, params.change)
		})
	}
}

func TestParseCanvasEditParams_Rejections(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"missing canvas", map[string]any{"markdown": "x"}, "canvas_id must be"},
		{"invalid canvas", map[string]any{"canvas_id": "xoxp-123-secret", "markdown": "x"}, "canvas_id must be"},
		{"unknown operation", map[string]any{"canvas_id": "F0123ABCD", "operation": "append", "markdown": "x"}, "operation must be one of"},
		{"append without markdown", map[string]any{"canvas_id": "F0123ABCD", "markdown": "  "}, "requires non-empty markdown"},
		{"insert_after without section", map[string]any{"canvas_id": "F0123ABCD", "operation": "insert_after", "markdown": "x"}, "requires section_id"},
		{"delete without section", map[string]any{"canvas_id": "F0123ABCD", "operation": "delete"}, "requires section_id"},
		{"delete with markdown", map[string]any{"canvas_id": "F0123ABCD", "operation": "delete", "section_id": "temp:C:a", "markdown": "x"}, "does not accept markdown"},
		{"unconfirmed whole replace", map[string]any{"canvas_id": "F0123ABCD", "operation": "replace", "markdown": "x"}, "replace_entire_canvas=true"},
		{"confirm flag on append", map[string]any{"canvas_id": "F0123ABCD", "replace_entire_canvas": true, "markdown": "x"}, "replace_entire_canvas applies only"},
		{"confirm flag with section", map[string]any{"canvas_id": "F0123ABCD", "operation": "replace", "section_id": "temp:C:a", "replace_entire_canvas": true, "markdown": "x"}, "replace_entire_canvas applies only"},
		{"append with section", map[string]any{"canvas_id": "F0123ABCD", "section_id": "temp:C:a", "markdown": "x"}, "does not accept section_id"},
		{"invalid section", map[string]any{"canvas_id": "F0123ABCD", "operation": "delete", "section_id": "https://x/?token=xoxp-1"}, "section_id must be"},
		{"rename without title", map[string]any{"canvas_id": "F0123ABCD", "operation": "rename"}, "requires a non-empty title"},
		{"title on append", map[string]any{"canvas_id": "F0123ABCD", "markdown": "x", "title": "t"}, "does not accept title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCanvasEditParams(canvasRequest(tt.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), "xoxp", "rejected input must not be echoed")
		})
	}
}

func TestCanvasesEditHandler_Success(t *testing.T) {
	api := &fakeCanvasSlackAPI{}
	result, err := canvasesHandler(api).CanvasesEditHandler(context.Background(), canvasRequest(map[string]any{
		"canvas_id": "F0123ABCD",
		"markdown":  "## Appended",
	}))
	require.NoError(t, err)
	require.Len(t, api.edits, 1, "exactly one Slack edit per call")
	assert.Equal(t, "F0123ABCD", api.editIDs[0])
	text := resultText(t, result)
	assert.Contains(t, text, "operation=insert_at_end")
	assert.Contains(t, text, "markdown_bytes=11")
	assert.Contains(t, text, "attachment_get_data file_id=F0123ABCD")
}

func TestCanvasesEditHandler_InvalidInputMakesNoCall(t *testing.T) {
	api := &fakeCanvasSlackAPI{}
	_, err := canvasesHandler(api).CanvasesEditHandler(context.Background(), canvasRequest(map[string]any{
		"canvas_id": "F0123ABCD",
		"operation": "replace",
		"markdown":  "everything",
	}))
	require.Error(t, err)
	assert.Empty(t, api.edits)
}

func TestMapCanvasError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		method string
		want   []string
	}{
		{"missing scope with needed", &provider.CanvasAPIError{Code: "missing_scope", Needed: "canvases:write"}, "canvases.edit",
			[]string{"missing_scope", "canvases:write", "reinstall the Slack app", "replace the server's token file"}},
		{"missing scope without needed", &provider.CanvasAPIError{Code: "missing_scope"}, "canvases.edit",
			[]string{"canvases:write (edit) / canvases:read (lookup)"}},
		{"not found", &provider.CanvasAPIError{Code: "canvas_not_found"}, "canvases.edit",
			[]string{"canvas_not_found", "F0123ABCD", "not visible to the connected identity"}},
		{"access denied", &provider.CanvasAPIError{Code: "access_denied"}, "canvases.edit",
			[]string{"access_denied", "cannot access canvas F0123ABCD"}},
		{"invalid auth", &provider.CanvasAPIError{Code: "invalid_auth"}, "canvases.edit",
			[]string{"invalid_auth", "credentials are invalid"}},
		{"rate limited", &provider.CanvasAPIError{Code: "ratelimited", RetryAfter: 30 * time.Second}, "canvases.edit",
			[]string{"retry after 30s"}},
		{"other code", &provider.CanvasAPIError{Code: "canvas_disabled_user_team"}, "canvases.edit",
			[]string{"Slack API error: canvas_disabled_user_team"}},
		{"edit transport failure", fmt.Errorf("wrapped: %w", provider.ErrCanvasTransport), "canvases.edit",
			[]string{"outcome unknown", "read the canvas back with attachment_get_data before retrying"}},
		{"lookup transport failure", fmt.Errorf("wrapped: %w", provider.ErrCanvasTransport), "canvases.sections.lookup",
			[]string{"Slack canvases.sections.lookup request failed"}},
		{"unclassified error", fmt.Errorf("Post https://x/api?token=xoxp-secret: boom"), "canvases.edit",
			[]string{"Slack canvases.edit request failed"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapCanvasError(tt.err, tt.method, "F0123ABCD").Error()
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			assert.NotContains(t, got, "xoxp")
		})
	}
}

func TestCanvasesSectionsLookupHandler(t *testing.T) {
	api := &fakeCanvasSlackAPI{sections: []string{"temp:C:one", "temp:C:two"}}
	result, err := canvasesHandler(api).CanvasesSectionsLookupHandler(context.Background(), canvasRequest(map[string]any{
		"canvas_id":     "F0123ABCD",
		"section_types": "h1, any_header",
		"contains_text": "Findings",
	}))
	require.NoError(t, err)
	assert.Equal(t, "section_id\ntemp:C:one\ntemp:C:two\n", resultText(t, result))
	assert.Equal(t, provider.CanvasSectionCriteria{SectionTypes: []string{"h1", "any_header"}, ContainsText: "Findings"}, api.lookupCrits[0])

	empty := &fakeCanvasSlackAPI{}
	result, err = canvasesHandler(empty).CanvasesSectionsLookupHandler(context.Background(), canvasRequest(map[string]any{
		"canvas_id":     "F0123ABCD",
		"section_types": "h2",
	}))
	require.NoError(t, err)
	assert.Equal(t, "section_id\n", resultText(t, result))
}

func TestCanvasesSectionsLookupHandler_Rejections(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"no criteria":  {"canvas_id": "F0123ABCD"},
		"bad type":     {"canvas_id": "F0123ABCD", "section_types": "h4"},
		"bad canvas":   {"canvas_id": "C0123ABCD", "section_types": "h1"},
		"blank types":  {"canvas_id": "F0123ABCD", "section_types": " , "},
		"missing all":  {},
		"lowercase id": {"canvas_id": "f0123abcd", "section_types": "h1"},
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeCanvasSlackAPI{}
			_, err := canvasesHandler(api).CanvasesSectionsLookupHandler(context.Background(), canvasRequest(args))
			require.Error(t, err)
			assert.Empty(t, api.lookupCrits)
		})
	}

	api := &fakeCanvasSlackAPI{lookupErr: &provider.CanvasAPIError{Code: "canvas_not_found"}}
	_, err := canvasesHandler(api).CanvasesSectionsLookupHandler(context.Background(), canvasRequest(map[string]any{
		"canvas_id": "F0123ABCD", "section_types": "any_header",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canvas_not_found")
}
