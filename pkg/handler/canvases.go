package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/korotovsky/slack-mcp-server/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

var (
	canvasIDRegex  = regexp.MustCompile(`^F[A-Z0-9]+$`)
	sectionIDRegex = regexp.MustCompile(`^[A-Za-z0-9:_.-]{1,128}$`)
)

const (
	canvasOpInsertAtEnd   = "insert_at_end"
	canvasOpInsertAtStart = "insert_at_start"
	canvasOpInsertAfter   = "insert_after"
	canvasOpInsertBefore  = "insert_before"
	canvasOpReplace       = "replace"
	canvasOpDelete        = "delete"
	canvasOpRename        = "rename"
)

var canvasSectionTypes = map[string]bool{"h1": true, "h2": true, "h3": true, "any_header": true}

type CanvasesHandler struct {
	slack   provider.SlackAPI
	isReady func() (bool, error)
	logger  *zap.Logger
}

func NewCanvasesHandler(apiProvider *provider.ApiProvider, logger *zap.Logger) *CanvasesHandler {
	return &CanvasesHandler{
		slack:   apiProvider.Slack(),
		isReady: apiProvider.IsReady,
		logger:  logger,
	}
}

type canvasEditParams struct {
	canvasID string
	change   provider.CanvasChange
}

// Rejected values are never echoed: callers may paste a token or private URL
// into the wrong argument.
func parseCanvasID(request mcp.CallToolRequest) (string, error) {
	canvasID := strings.TrimSpace(request.GetString("canvas_id", ""))
	if !canvasIDRegex.MatchString(canvasID) {
		return "", errors.New("canvas_id must be a Slack canvas ID in format Fxxxxxxxxxx")
	}
	return canvasID, nil
}

func parseCanvasEditParams(request mcp.CallToolRequest) (*canvasEditParams, error) {
	canvasID, err := parseCanvasID(request)
	if err != nil {
		return nil, err
	}

	operation := strings.TrimSpace(request.GetString("operation", canvasOpInsertAtEnd))
	if operation == "" {
		operation = canvasOpInsertAtEnd
	}
	markdown := request.GetString("markdown", "")
	sectionID := strings.TrimSpace(request.GetString("section_id", ""))
	title := request.GetString("title", "")
	replaceEntire := request.GetBool("replace_entire_canvas", false)

	if sectionID != "" && !sectionIDRegex.MatchString(sectionID) {
		return nil, errors.New("section_id must be a section ID returned by canvases_sections_lookup")
	}

	needsMarkdown, needsSection, allowsSection := false, false, false
	switch operation {
	case canvasOpInsertAtEnd, canvasOpInsertAtStart:
		needsMarkdown = true
	case canvasOpInsertAfter, canvasOpInsertBefore:
		needsMarkdown, needsSection, allowsSection = true, true, true
	case canvasOpReplace:
		needsMarkdown, allowsSection = true, true
		if sectionID == "" && !replaceEntire {
			return nil, errors.New("replace without section_id overwrites the entire canvas; pass replace_entire_canvas=true to confirm, or use canvases_sections_lookup to target one section")
		}
	case canvasOpDelete:
		needsSection, allowsSection = true, true
	case canvasOpRename:
		if strings.TrimSpace(title) == "" {
			return nil, errors.New("rename requires a non-empty title")
		}
	default:
		return nil, fmt.Errorf("operation must be one of: %s", strings.Join([]string{
			canvasOpInsertAtEnd, canvasOpInsertAtStart, canvasOpInsertAfter, canvasOpInsertBefore,
			canvasOpReplace, canvasOpDelete, canvasOpRename,
		}, ", "))
	}

	if needsMarkdown && strings.TrimSpace(markdown) == "" {
		return nil, fmt.Errorf("%s requires non-empty markdown", operation)
	}
	if !needsMarkdown && markdown != "" {
		return nil, fmt.Errorf("%s does not accept markdown", operation)
	}
	if needsSection && sectionID == "" {
		return nil, fmt.Errorf("%s requires section_id; find one with canvases_sections_lookup", operation)
	}
	if !allowsSection && sectionID != "" {
		return nil, fmt.Errorf("%s does not accept section_id", operation)
	}
	if operation != canvasOpRename && title != "" {
		return nil, fmt.Errorf("%s does not accept title; use operation=rename", operation)
	}
	if replaceEntire && (operation != canvasOpReplace || sectionID != "") {
		return nil, errors.New("replace_entire_canvas applies only to replace without section_id")
	}

	change := provider.CanvasChange{Operation: operation, SectionID: sectionID}
	if needsMarkdown {
		change.DocumentContent = &provider.CanvasContent{Type: "markdown", Markdown: markdown}
	}
	if operation == canvasOpRename {
		change.TitleContent = &provider.CanvasContent{Type: "markdown", Markdown: title}
	}
	return &canvasEditParams{canvasID: canvasID, change: change}, nil
}

// mapCanvasError returns fixed guidance keyed on a sanitized Slack code. The
// canvas ID has already passed validation, so echoing it is safe.
func mapCanvasError(err error, method, canvasID string) error {
	var apiErr *provider.CanvasAPIError
	if errors.As(err, &apiErr) {
		code := apiErr.Code
		switch code {
		case "missing_scope":
			needed := apiErr.Needed
			if needed == "" {
				needed = "canvases:write (edit) / canvases:read (lookup)"
			}
			return fmt.Errorf("Slack API error: missing_scope: the Slack token lacks %s; add it under User Token Scopes, reinstall the Slack app, then replace the server's token file and restart the service", needed)
		case "canvas_not_found":
			return fmt.Errorf("Slack API error: canvas_not_found: canvas %s does not exist or is not visible to the connected identity", canvasID)
		case "access_denied", "not_allowed", "restricted_action", "not_in_channel":
			return fmt.Errorf("Slack API error: %s: the connected identity cannot access canvas %s; ask the canvas owner to grant edit access", code, canvasID)
		case "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive":
			return fmt.Errorf("Slack API error: %s: connector credentials are invalid; replace the Slack token and restart the service", code)
		case "ratelimited":
			if apiErr.RetryAfter > 0 {
				return fmt.Errorf("Slack API error: ratelimited: retry after %ds", int(apiErr.RetryAfter.Seconds()))
			}
			return errors.New("Slack API error: ratelimited: retry later")
		default:
			return fmt.Errorf("Slack API error: %s", code)
		}
	}
	if errors.Is(err, provider.ErrCanvasTransport) {
		if method == "canvases.edit" {
			return fmt.Errorf("Slack canvases.edit outcome unknown: %s; read the canvas back with attachment_get_data before retrying", err.Error())
		}
		return fmt.Errorf("Slack %s request failed: %s", method, err.Error())
	}
	return fmt.Errorf("Slack %s request failed", method)
}

func (h *CanvasesHandler) CanvasesEditHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ready, err := h.isReady(); !ready {
		h.logger.Error("API provider not ready", zap.Error(err))
		return nil, err
	}

	params, err := parseCanvasEditParams(request)
	if err != nil {
		return nil, err
	}

	if err := h.slack.EditCanvasContext(ctx, params.canvasID, params.change); err != nil {
		mapped := mapCanvasError(err, "canvases.edit", params.canvasID)
		h.logger.Warn("canvases.edit failed",
			zap.String("canvas_id", params.canvasID),
			zap.String("operation", params.change.Operation),
			zap.String("error", mapped.Error()),
		)
		return nil, mapped
	}

	markdownBytes := 0
	if params.change.DocumentContent != nil {
		markdownBytes = len(params.change.DocumentContent.Markdown)
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Canvas %s edited: operation=%s, markdown_bytes=%d.\nVerify with attachment_get_data file_id=%s.",
		params.canvasID, params.change.Operation, markdownBytes, params.canvasID,
	)), nil
}

func (h *CanvasesHandler) CanvasesSectionsLookupHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ready, err := h.isReady(); !ready {
		h.logger.Error("API provider not ready", zap.Error(err))
		return nil, err
	}

	canvasID, err := parseCanvasID(request)
	if err != nil {
		return nil, err
	}

	var criteria provider.CanvasSectionCriteria
	for _, sectionType := range strings.Split(request.GetString("section_types", ""), ",") {
		sectionType = strings.TrimSpace(sectionType)
		if sectionType == "" {
			continue
		}
		if !canvasSectionTypes[sectionType] {
			return nil, errors.New("section_types must be a comma-separated list of: h1, h2, h3, any_header")
		}
		criteria.SectionTypes = append(criteria.SectionTypes, sectionType)
	}
	criteria.ContainsText = request.GetString("contains_text", "")
	if len(criteria.SectionTypes) == 0 && criteria.ContainsText == "" {
		return nil, errors.New("provide section_types and/or contains_text")
	}

	ids, err := h.slack.LookupCanvasSectionsContext(ctx, canvasID, criteria)
	if err != nil {
		return nil, mapCanvasError(err, "canvases.sections.lookup", canvasID)
	}

	var b strings.Builder
	b.WriteString("section_id\n")
	for _, id := range ids {
		b.WriteString(id)
		b.WriteString("\n")
	}
	return mcp.NewToolResultText(b.String()), nil
}
