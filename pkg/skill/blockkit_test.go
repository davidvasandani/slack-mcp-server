package skill

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/stretchr/testify/require"
)

func TestUnitBlockKitSkillIsExposedThroughMCP(t *testing.T) {
	ctx := context.Background()
	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()
	Register(srv)
	require.NoError(t, srv.Start(ctx))

	resources, err := srv.Client().ListResources(ctx, mcp.ListResourcesRequest{})
	require.NoError(t, err)
	require.Contains(t, resourceURIs(resources.Resources), BlockKitResourceURI)

	var resourceRequest mcp.ReadResourceRequest
	resourceRequest.Params.URI = BlockKitResourceURI
	resource, err := srv.Client().ReadResource(ctx, resourceRequest)
	require.NoError(t, err)
	require.Len(t, resource.Contents, 1)
	resourceContent, ok := resource.Contents[0].(mcp.TextResourceContents)
	require.True(t, ok)
	require.Contains(t, resourceContent.Text, "# Slack Block Kit UI")

	resourceRequest.Params.URI = BlockKitSchemaPrefix + "blocks--actions-block.md"
	schema, err := srv.Client().ReadResource(ctx, resourceRequest)
	require.NoError(t, err)
	require.Len(t, schema.Contents, 1)
	schemaContent, ok := schema.Contents[0].(mcp.TextResourceContents)
	require.True(t, ok)
	require.Contains(t, schemaContent.Text, "maximum of 25 elements")

	prompts, err := srv.Client().ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(t, err)
	require.Contains(t, promptNames(prompts.Prompts), BlockKitPromptName)

	var promptRequest mcp.GetPromptRequest
	promptRequest.Params.Name = BlockKitPromptName
	promptRequest.Params.Arguments = map[string]string{"task": "Show a deployment result."}
	prompt, err := srv.Client().GetPrompt(ctx, promptRequest)
	require.NoError(t, err)
	require.Len(t, prompt.Messages, 1)
	content, ok := prompt.Messages[0].Content.(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, "Show a deployment result.")
	require.Contains(t, content.Text, BlockKitSchemaPrefix+"blocks--section-block.md")
}

func TestUnitBlockKitSchemaResourcesUseEmbeddedOfficialMarkdown(t *testing.T) {
	content, err := BlockKitSchema("blocks--actions-block.md")
	require.NoError(t, err)
	require.Contains(t, content, "Source: https://docs.slack.dev/reference/block-kit/blocks/actions-block")
	require.Contains(t, content, "maximum of 25 elements")

	_, err = BlockKitSchema("../../SKILL.md")
	require.Error(t, err)
}

func resourceURIs(resources []mcp.Resource) []string {
	result := make([]string, 0, len(resources))
	for _, resource := range resources {
		result = append(result, resource.URI)
	}
	return result
}

func promptNames(prompts []mcp.Prompt) []string {
	result := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		result = append(result, prompt.Name)
	}
	return result
}

func TestUnitBlockKitSkillContainsNoRailsSpecificContract(t *testing.T) {
	content, err := BlockKitSkill()
	require.NoError(t, err)
	for _, excluded := range []string{"WidgetTemplateService", "SlackTaskResponseService", "Rails.logger"} {
		require.False(t, strings.Contains(content, excluded), "unexpected Rails-specific reference %q", excluded)
	}
}
