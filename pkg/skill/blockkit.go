package skill

import (
	"context"
	"embed"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	BlockKitPromptName      = "slack_blockkit_ui"
	BlockKitResourceURI     = "slack://skills/slack-blockkit-ui"
	BlockKitSchemaPrefix    = BlockKitResourceURI + "/schemas/"
	blockKitSkillPath       = "slack-blockkit-ui/SKILL.md"
	blockKitReferencePath   = "slack-blockkit-ui/references/block-kit-reference.md"
	blockKitPatternsPath    = "slack-blockkit-ui/references/patterns.md"
	blockKitSchemaDirectory = "slack-blockkit-ui/references/schemas"
)

var schemaNamePattern = regexp.MustCompile(`^(blocks|block-elements|composition-objects)--[a-z0-9-]+\.md$`)

//go:embed slack-blockkit-ui/SKILL.md slack-blockkit-ui/references/*.md slack-blockkit-ui/references/schemas/*.md
var blockKitFiles embed.FS

type registrar interface {
	AddResource(mcp.Resource, server.ResourceHandlerFunc)
	AddResourceTemplate(mcp.ResourceTemplate, server.ResourceTemplateHandlerFunc)
	AddPrompt(mcp.Prompt, server.PromptHandlerFunc)
}

func readEmbedded(name string) (string, error) {
	content, err := blockKitFiles.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read embedded Block Kit skill file %q: %w", name, err)
	}
	return string(content), nil
}

func BlockKitSkill() (string, error) {
	parts := make([]string, 0, 3)
	for _, name := range []string{blockKitSkillPath, blockKitReferencePath, blockKitPatternsPath} {
		content, err := readEmbedded(name)
		if err != nil {
			return "", err
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n"), nil
}

func BlockKitSchema(name string) (string, error) {
	if !schemaNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid Block Kit schema resource %q", name)
	}
	return readEmbedded(path.Join(blockKitSchemaDirectory, name))
}

func Register(s registrar) {
	s.AddResource(mcp.NewResource(
		BlockKitResourceURI,
		"Slack Block Kit UI skill",
		mcp.WithResourceDescription("Instructions and current official schema index for composing accessible Slack Block Kit JSON."),
		mcp.WithMIMEType("text/markdown"),
	), blockKitSkillResource)

	s.AddResourceTemplate(mcp.NewResourceTemplate(
		BlockKitSchemaPrefix+"{name}",
		"Slack Block Kit schema",
		mcp.WithTemplateDescription("Current official Slack schema for one Block Kit block, element, or composition object."),
		mcp.WithTemplateMIMEType("text/markdown"),
	), blockKitSchemaResource)

	s.AddPrompt(mcp.NewPrompt(
		BlockKitPromptName,
		mcp.WithPromptDescription("Design valid, accessible Slack Block Kit JSON using the bundled current official schema index."),
		mcp.WithArgument("task", mcp.ArgumentDescription("Optional description of the Slack message or UI to design.")),
	), blockKitPrompt)
}

func blockKitSkillResource(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	content, err := BlockKitSkill()
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      BlockKitResourceURI,
		MIMEType: "text/markdown",
		Text:     content,
	}}, nil
}

func blockKitSchemaResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	if !strings.HasPrefix(request.Params.URI, BlockKitSchemaPrefix) {
		return nil, fmt.Errorf("unsupported Block Kit schema URI %q", request.Params.URI)
	}
	name := strings.TrimPrefix(request.Params.URI, BlockKitSchemaPrefix)
	content, err := BlockKitSchema(name)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{mcp.TextResourceContents{
		URI:      request.Params.URI,
		MIMEType: "text/markdown",
		Text:     content,
	}}, nil
}

func blockKitPrompt(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	content, err := BlockKitSkill()
	if err != nil {
		return nil, err
	}
	if task := strings.TrimSpace(request.Params.Arguments["task"]); task != "" {
		content += "\n\n## Requested UI\n\n" + task
	}
	return mcp.NewGetPromptResult(
		"Design valid, accessible Slack Block Kit JSON",
		[]mcp.PromptMessage{mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(content))},
	), nil
}
