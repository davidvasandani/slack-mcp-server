package handler

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/korotovsky/slack-mcp-server/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeAttachmentSlackAPI struct {
	provider.SlackAPI
	fileInfo       *slack.File
	fileInfoErr    error
	fileContent    []byte
	fileGetErr     error
	replyPages     [][]slack.Message
	replyPageIndex int
	replyParams    []slack.GetConversationRepliesParameters
}

func (f *fakeAttachmentSlackAPI) GetFileInfoContext(context.Context, string, int, int) (*slack.File, []slack.Comment, *slack.Paging, error) {
	return f.fileInfo, nil, nil, f.fileInfoErr
}

func (f *fakeAttachmentSlackAPI) GetFileContext(_ context.Context, _ string, writer io.Writer) error {
	if f.fileGetErr != nil {
		return f.fileGetErr
	}
	_, err := writer.Write(f.fileContent)
	return err
}

func (f *fakeAttachmentSlackAPI) GetConversationRepliesContext(
	_ context.Context,
	params *slack.GetConversationRepliesParameters,
) ([]slack.Message, bool, string, error) {
	f.replyParams = append(f.replyParams, *params)
	page := f.replyPages[f.replyPageIndex]
	f.replyPageIndex++
	hasMore := f.replyPageIndex < len(f.replyPages)
	if hasMore {
		return page, true, "next-page", nil
	}
	return page, false, "", nil
}

func attachmentRequest(fileID string) mcp.CallToolRequest {
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]any{"file_id": fileID}
	return request
}

func attachmentHandler(api provider.SlackAPI) *ConversationsHandler {
	return &ConversationsHandler{
		slack:   api,
		isReady: func() (bool, error) { return true, nil },
		logger:  zap.NewNop(),
	}
}

func TestParseParamsToolFilesGet(t *testing.T) {
	handler := attachmentHandler(&fakeAttachmentSlackAPI{})

	for _, fileID := range []string{"", "not-a-file", "f1234abcd", "F1234-abcd"} {
		_, err := handler.parseParamsToolFilesGet(attachmentRequest(fileID))
		require.Error(t, err)
		if fileID != "" {
			assert.NotContains(t, err.Error(), fileID, "invalid input must not be echoed")
		}
	}

	params, err := handler.parseParamsToolFilesGet(attachmentRequest(" F1234ABCD "))
	require.NoError(t, err)
	assert.Equal(t, "F1234ABCD", params.fileID)
}

func TestFilesGetHandler_TextAndBinary(t *testing.T) {
	tests := []struct {
		name     string
		mimetype string
		content  []byte
		encoding string
		mode     string
	}{
		{name: "text", mimetype: "text/plain", content: []byte("hello"), encoding: `"encoding":"none"`},
		{name: "pdf", mimetype: "application/pdf", content: []byte("%PDF"), encoding: `"encoding":"base64"`},
		{name: "Slackbot email", mimetype: "text/html", content: []byte("<p>email body</p>"), encoding: `"encoding":"none"`, mode: "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAttachmentSlackAPI{
				fileInfo: &slack.File{
					ID:                 "F1234ABCD",
					Name:               "example",
					Mimetype:           tt.mimetype,
					Size:               len(tt.content),
					Mode:               tt.mode,
					URLPrivateDownload: "https://files.slack.com/private/secret",
				},
				fileContent: tt.content,
			}
			result, err := attachmentHandler(api).FilesGetHandler(context.Background(), attachmentRequest("F1234ABCD"))
			require.NoError(t, err)
			require.Len(t, result.Content, 1)
			textResult, ok := result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			assert.Contains(t, textResult.Text, tt.encoding)
			assert.NotContains(t, textResult.Text, "files.slack.com")
		})
	}
}

func TestMessageFileMetadataFixtures(t *testing.T) {
	fixtures := []struct {
		name  string
		files []slack.File
		want  string
	}{
		{
			name:  "public channel",
			files: []slack.File{{ID: "F1PUBLIC", Name: "public.txt"}},
			want:  "F1PUBLIC (public.txt)",
		},
		{
			name:  "private channel",
			files: []slack.File{{ID: "F2PRIVATE", Name: "private.pdf"}},
			want:  "F2PRIVATE (private.pdf)",
		},
		{
			name:  "thread reply",
			files: []slack.File{{ID: "F3THREAD", Name: "thread.png"}},
			want:  "F3THREAD (thread.png)",
		},
		{
			name:  "Slackbot email",
			files: []slack.File{{ID: "F4EMAIL", Name: "Re: TAM Introduction - sweetgreen", Mode: "email", Filetype: "email"}},
			want:  "F4EMAIL (Re: TAM Introduction - sweetgreen)",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			count, IDs, hasMedia := messageFileMetadata(fixture.files, slack.Blocks{})
			assert.Equal(t, 1, count)
			assert.Equal(t, fixture.want, IDs)
			assert.True(t, hasMedia)
		})
	}
}

func TestFilesGetHandler_Image(t *testing.T) {
	api := &fakeAttachmentSlackAPI{
		fileInfo: &slack.File{
			ID:                 "F1234ABCD",
			Name:               "image.png",
			Mimetype:           "image/png",
			Size:               3,
			URLPrivateDownload: "https://files.slack.com/private/secret",
		},
		fileContent: []byte("png"),
	}
	result, err := attachmentHandler(api).FilesGetHandler(context.Background(), attachmentRequest("F1234ABCD"))
	require.NoError(t, err)
	require.Len(t, result.Content, 2)
	_, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	_, ok = result.Content[1].(mcp.ImageContent)
	require.True(t, ok)
}

func TestFilesGetHandler_RejectionsAreActionableAndSecretSafe(t *testing.T) {
	tests := []struct {
		name string
		api  *fakeAttachmentSlackAPI
		want string
	}{
		{
			name: "missing scope",
			api:  &fakeAttachmentSlackAPI{fileInfoErr: errors.New("missing_scope")},
			want: "OAuth scope",
		},
		{
			name: "access denied",
			api:  &fakeAttachmentSlackAPI{fileInfoErr: errors.New("access_denied")},
			want: "connected identity",
		},
		{
			name: "deleted",
			api:  &fakeAttachmentSlackAPI{fileInfoErr: errors.New("file_deleted")},
			want: "deleted",
		},
		{
			name: "external",
			api: &fakeAttachmentSlackAPI{fileInfo: &slack.File{
				ID: "F1234ABCD", Name: "external", Mimetype: "text/plain", Mode: "external",
			}},
			want: "externally hosted",
		},
		{
			name: "metadata too large",
			api: &fakeAttachmentSlackAPI{fileInfo: &slack.File{
				ID: "F1234ABCD", Name: "large.pdf", Mimetype: "application/pdf", Size: maxFileSizeBytes + 1,
			}},
			want: "exceeds",
		},
		{
			name: "unsupported type",
			api: &fakeAttachmentSlackAPI{fileInfo: &slack.File{
				ID: "F1234ABCD", Name: "unknown", Mimetype: "", Size: 10,
			}},
			want: "unsupported MIME type",
		},
		{
			name: "bounded download",
			api: &fakeAttachmentSlackAPI{
				fileInfo: &slack.File{
					ID: "F1234ABCD", Name: "fake.txt", Mimetype: "text/plain", Size: 1,
					URLPrivateDownload: "https://files.slack.com/private/xoxp-secret",
				},
				fileContent: make([]byte, maxFileSizeBytes+1),
			},
			want: "during download",
		},
		{
			name: "sanitized HTTP error",
			api: &fakeAttachmentSlackAPI{
				fileInfo: &slack.File{
					ID: "F1234ABCD", Name: "private.txt", Mimetype: "text/plain", Size: 1,
					URLPrivateDownload: "https://files.slack.com/private/xoxp-secret",
				},
				fileGetErr: slack.StatusCodeError{Code: 403, Status: "forbidden https://files.slack.com/xoxp-secret"},
			},
			want: "HTTP status 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := attachmentHandler(tt.api).FilesGetHandler(context.Background(), attachmentRequest("F1234ABCD"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			for _, secret := range []string{"xoxb-", "xoxc-", "xoxd-", "xoxp-", "xoxe-", "files.slack.com"} {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestParseSlackMessagePermalink(t *testing.T) {
	permalink, ok := parseSlackMessagePermalink("https://sweetgreen.slack.com/archives/C0BE62MCDU6/p1784648794618929")
	require.True(t, ok)
	assert.Equal(t, "C0BE62MCDU6", permalink.channelID)
	assert.Equal(t, "1784648794.618929", permalink.messageTS)
	assert.Equal(t, permalink.messageTS, permalink.threadTS)

	reply, ok := parseSlackMessagePermalink("https://sweetgreen.slack.com/archives/C123/p1784648794618929?thread_ts=1784640000.000001")
	require.True(t, ok)
	assert.Equal(t, "1784640000.000001", reply.threadTS)

	for _, invalid := range []string{"not a url", "https://example.com/archives/C123/p1784648794618929", "https://sweetgreen.slack.com/archives/C123/nope"} {
		_, ok := parseSlackMessagePermalink(invalid)
		assert.False(t, ok)
	}
}

func TestGetMessageByPermalinkPaginatesAndPreservesFiles(t *testing.T) {
	api := &fakeAttachmentSlackAPI{
		replyPages: [][]slack.Message{
			{{Msg: slack.Msg{Timestamp: "1784640000.000001"}}},
			{{
				Msg: slack.Msg{
					Timestamp: "1784648794.618929",
					Files: []slack.File{{
						ID: "F0BJX4Y3N5A", Name: "Re: TAM Introduction - sweetgreen",
					}},
				},
			}},
		},
	}
	handler := attachmentHandler(api)
	message, err := handler.getMessageByPermalink(context.Background(), slackMessagePermalink{
		channelID: "C0BE62MCDU6",
		messageTS: "1784648794.618929",
		threadTS:  "1784640000.000001",
	})
	require.NoError(t, err)
	require.Len(t, message.Files, 1)
	assert.Equal(t, "F0BJX4Y3N5A", message.Files[0].ID)
	require.Len(t, api.replyParams, 2)
	assert.Equal(t, "next-page", api.replyParams[1].Cursor)
}

func TestLimitedWriterRejectsOversizedSingleWrite(t *testing.T) {
	writer := &limitedWriter{buf: nil, limit: 2}
	_, err := writer.Write([]byte("abc"))
	require.ErrorIs(t, err, errDownloadSizeExceeded)
	assert.Zero(t, writer.written)
}
