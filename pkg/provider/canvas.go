package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// slack-go's CanvasChange always serializes document_content and has no
// title_content, so it cannot express the documented delete and rename
// operations. These wire types omit every field an operation does not use.
type CanvasContent struct {
	Type     string `json:"type"`
	Markdown string `json:"markdown"`
}

type CanvasChange struct {
	Operation       string         `json:"operation"`
	SectionID       string         `json:"section_id,omitempty"`
	DocumentContent *CanvasContent `json:"document_content,omitempty"`
	TitleContent    *CanvasContent `json:"title_content,omitempty"`
}

type CanvasSectionCriteria struct {
	SectionTypes []string `json:"section_types,omitempty"`
	ContainsText string   `json:"contains_text,omitempty"`
}

// CanvasAPIError is a Slack method failure reduced to fields that are safe to
// return: a sanitized error code, the scopes Slack reports as needed, and the
// retry delay for rate limiting.
type CanvasAPIError struct {
	Code       string
	Needed     string
	RetryAfter time.Duration
}

func (e *CanvasAPIError) Error() string { return e.Code }

// ErrCanvasTransport marks a request whose outcome the caller cannot know: it
// may have been applied by Slack even though no valid response arrived.
var ErrCanvasTransport = errors.New("canvas request transport failure")

type canvasTransportError struct{ category string }

func (e *canvasTransportError) Error() string { return e.category }
func (e *canvasTransportError) Unwrap() error { return ErrCanvasTransport }

var (
	slackErrorCodeRegex = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	slackScopeListRegex = regexp.MustCompile(`^[a-z0-9_.:,]{1,256}$`)
)

func sanitizeSlackCode(code string) string {
	if slackErrorCodeRegex.MatchString(code) {
		return code
	}
	return "unknown_error"
}

type canvasResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Needed   string `json:"needed"`
	Sections []struct {
		ID string `json:"id"`
	} `json:"sections"`
}

// callCanvasMethod makes exactly one form POST. Neither the request body (which
// carries the token) nor the transport error text (which can embed request
// details) is ever returned: failures collapse to fixed categories.
func (c *MCPSlackClient) callCanvasMethod(ctx context.Context, method string, values url.Values) (*canvasResponse, error) {
	values.Set("token", c.authProvider.SlackToken())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+method, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, &canvasTransportError{category: "request could not be built"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &canvasTransportError{category: "request cancelled or timed out"}
		}
		return nil, &canvasTransportError{category: "network error"}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return nil, &CanvasAPIError{Code: "ratelimited", RetryAfter: time.Duration(retry) * time.Second}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &canvasTransportError{category: fmt.Sprintf("HTTP status %d", resp.StatusCode)}
	}

	var out canvasResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, &canvasTransportError{category: "malformed response"}
	}
	if !out.OK {
		apiErr := &CanvasAPIError{Code: sanitizeSlackCode(out.Error)}
		if slackScopeListRegex.MatchString(out.Needed) {
			apiErr.Needed = out.Needed
		}
		return nil, apiErr
	}
	return &out, nil
}

// EditCanvasContext applies one change with canvases.edit. It is never retried:
// an edit is not idempotent (a replayed append duplicates content).
func (c *MCPSlackClient) EditCanvasContext(ctx context.Context, canvasID string, change CanvasChange) error {
	changes, err := json.Marshal([]CanvasChange{change})
	if err != nil {
		return err
	}
	_, err = c.callCanvasMethod(ctx, "canvases.edit", url.Values{
		"canvas_id": {canvasID},
		"changes":   {string(changes)},
	})
	return err
}

// LookupCanvasSectionsContext returns section IDs from canvases.sections.lookup.
func (c *MCPSlackClient) LookupCanvasSectionsContext(ctx context.Context, canvasID string, criteria CanvasSectionCriteria) ([]string, error) {
	encoded, err := json.Marshal(criteria)
	if err != nil {
		return nil, err
	}
	out, err := c.callCanvasMethod(ctx, "canvases.sections.lookup", url.Values{
		"canvas_id": {canvasID},
		"criteria":  {string(encoded)},
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Sections))
	for _, section := range out.Sections {
		ids = append(ids, section.ID)
	}
	return ids, nil
}
