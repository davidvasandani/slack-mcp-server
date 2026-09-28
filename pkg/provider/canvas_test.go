package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/rusq/slackdump/v3/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canvasTestToken = "xoxp-secret-fixture"

type recordedCanvasRequest struct {
	path string
	form url.Values
}

func canvasTestClient(t *testing.T, handler http.HandlerFunc) (*MCPSlackClient, *[]recordedCanvasRequest) {
	t.Helper()
	var requests []recordedCanvasRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		requests = append(requests, recordedCanvasRequest{path: r.URL.Path, form: r.PostForm})
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	authProvider, err := auth.NewValueAuth(canvasTestToken, "")
	require.NoError(t, err)
	return &MCPSlackClient{
		httpClient:   server.Client(),
		apiURL:       server.URL + "/api/",
		authProvider: authProvider,
	}, &requests
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestEditCanvasContext_WireShape(t *testing.T) {
	tests := []struct {
		name   string
		change CanvasChange
		want   string
	}{
		{
			name:   "append",
			change: CanvasChange{Operation: "insert_at_end", DocumentContent: &CanvasContent{Type: "markdown", Markdown: "# Hi"}},
			want:   `[{"operation":"insert_at_end","document_content":{"type":"markdown","markdown":"# Hi"}}]`,
		},
		{
			name:   "insert after section",
			change: CanvasChange{Operation: "insert_after", SectionID: "temp:C:abc", DocumentContent: &CanvasContent{Type: "markdown", Markdown: "x"}},
			want:   `[{"operation":"insert_after","section_id":"temp:C:abc","document_content":{"type":"markdown","markdown":"x"}}]`,
		},
		{
			name:   "delete sends no document_content",
			change: CanvasChange{Operation: "delete", SectionID: "temp:C:abc"},
			want:   `[{"operation":"delete","section_id":"temp:C:abc"}]`,
		},
		{
			name:   "rename sends title_content",
			change: CanvasChange{Operation: "rename", TitleContent: &CanvasContent{Type: "markdown", Markdown: "New"}},
			want:   `[{"operation":"rename","title_content":{"type":"markdown","markdown":"New"}}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, requests := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, `{"ok":true}`)
			})

			require.NoError(t, client.EditCanvasContext(context.Background(), "F0123ABCD", tt.change))
			require.Len(t, *requests, 1)
			got := (*requests)[0]
			assert.Equal(t, "/api/canvases.edit", got.path)
			assert.Equal(t, "F0123ABCD", got.form.Get("canvas_id"))
			assert.Equal(t, canvasTestToken, got.form.Get("token"))
			assert.JSONEq(t, tt.want, got.form.Get("changes"))
		})
	}
}

func TestEditCanvasContext_SlackErrors(t *testing.T) {
	t.Run("missing_scope carries sanitized needed scopes", func(t *testing.T) {
		client, _ := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"canvases:write","provided":"canvases:read"}`)
		})
		err := client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		var apiErr *CanvasAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "missing_scope", apiErr.Code)
		assert.Equal(t, "canvases:write", apiErr.Needed)
	})

	t.Run("unexpected error code and needed value are sanitized", func(t *testing.T) {
		client, _ := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, `{"ok":false,"error":"Bad <code> xoxp-leak","needed":"xoxp-leak <script>"}`)
		})
		err := client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		var apiErr *CanvasAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "unknown_error", apiErr.Code)
		assert.Empty(t, apiErr.Needed)
	})

	t.Run("HTTP 429 reports Retry-After and is not retried", func(t *testing.T) {
		client, requests := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		err := client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		var apiErr *CanvasAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "ratelimited", apiErr.Code)
		assert.Equal(t, 30*time.Second, apiErr.RetryAfter)
		assert.Len(t, *requests, 1)
	})

	t.Run("5xx is an unknown-outcome transport failure", func(t *testing.T) {
		client, requests := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		})
		err := client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		require.True(t, errors.Is(err, ErrCanvasTransport))
		assert.Equal(t, "HTTP status 502", err.Error())
		assert.Len(t, *requests, 1, "edits must not be retried automatically")
	})

	t.Run("malformed body is a transport failure", func(t *testing.T) {
		client, _ := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, `not json`)
		})
		err := client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		require.True(t, errors.Is(err, ErrCanvasTransport))
	})

	t.Run("network errors never include the request or token", func(t *testing.T) {
		authProvider, err := auth.NewValueAuth(canvasTestToken, "")
		require.NoError(t, err)
		client := &MCPSlackClient{
			httpClient:   &http.Client{},
			apiURL:       "http://127.0.0.1:1/api/",
			authProvider: authProvider,
		}
		err = client.EditCanvasContext(context.Background(), "F0123ABCD", CanvasChange{Operation: "insert_at_end"})
		require.True(t, errors.Is(err, ErrCanvasTransport))
		assert.Equal(t, "network error", err.Error())
		assert.NotContains(t, err.Error(), canvasTestToken)
		assert.NotContains(t, err.Error(), "127.0.0.1")
	})
}

func TestLookupCanvasSectionsContext(t *testing.T) {
	client, requests := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"ok":true,"sections":[{"id":"temp:C:one"},{"id":"temp:C:two"}]}`)
	})

	ids, err := client.LookupCanvasSectionsContext(context.Background(), "F0123ABCD", CanvasSectionCriteria{
		SectionTypes: []string{"h2"},
		ContainsText: "Findings",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"temp:C:one", "temp:C:two"}, ids)

	got := (*requests)[0]
	assert.Equal(t, "/api/canvases.sections.lookup", got.path)
	var criteria map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.form.Get("criteria")), &criteria))
	assert.Equal(t, map[string]any{"section_types": []any{"h2"}, "contains_text": "Findings"}, criteria)
}

func TestLookupCanvasSectionsContext_NotFound(t *testing.T) {
	client, _ := canvasTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"ok":false,"error":"canvas_not_found"}`)
	})
	_, err := client.LookupCanvasSectionsContext(context.Background(), "F0123ABCD", CanvasSectionCriteria{SectionTypes: []string{"any_header"}})
	var apiErr *CanvasAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "canvas_not_found", apiErr.Code)
}
