package text

import (
	"fmt"
	"github.com/slack-go/slack"
	"strings"
)

// MessageBlocksToText preserves identifiers, link targets and formatting in
// rich-text blocks, while sharing the established layout extraction rules.
func MessageBlocksToText(blocks slack.Blocks) string {
	rendered := slack.Blocks{}
	for _, block := range blocks.BlockSet {
		if rich, ok := block.(*slack.RichTextBlock); ok {
			var parts []string
			for _, element := range rich.Elements {
				parts = append(parts, messageRichElement(element))
			}
			rendered.BlockSet = append(rendered.BlockSet, slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", strings.Join(parts, "\n"), false, false), nil, nil))
		} else {
			rendered.BlockSet = append(rendered.BlockSet, block)
		}
	}
	var parts []string
	for _, block := range rendered.BlockSet {
		if body := BlocksToText(slack.Blocks{BlockSet: []slack.Block{block}}); body != "" {
			parts = append(parts, body)
		}
	}
	return strings.Join(parts, "\n")
}

func messageRichElement(element slack.RichTextElement) string {
	switch e := element.(type) {
	case *slack.RichTextSection:
		return messageRichSection(e)
	case *slack.RichTextPreformatted:
		return "```\n" + messageRichSection(&e.RichTextSection) + "\n```"
	case *slack.RichTextQuote:
		return "> " + strings.ReplaceAll(messageRichSection((*slack.RichTextSection)(e)), "\n", "\n> ")
	case *slack.RichTextList:
		var lines []string
		for i, item := range e.Elements {
			prefix := "• "
			if string(e.Style) == "ordered" {
				prefix = fmt.Sprintf("%d. ", e.Offset+i+1)
			}
			lines = append(lines, strings.Repeat("  ", max(0, min(e.Indent, 20)))+prefix+messageRichElement(item))
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func messageRichStyle(value string, style *slack.RichTextSectionTextStyle) string {
	if style == nil || value == "" {
		return value
	}
	if style.Code {
		return "`" + value + "`"
	}
	if style.Bold {
		value = "*" + value + "*"
	}
	if style.Italic {
		value = "_" + value + "_"
	}
	if style.Strike {
		value = "~" + value + "~"
	}
	return value
}

func messageRichSection(section *slack.RichTextSection) string {
	var parts []string
	for _, element := range section.Elements {
		value := ""
		var style *slack.RichTextSectionTextStyle
		switch e := element.(type) {
		case *slack.RichTextSectionTextElement:
			value = e.Text
			style = e.Style
		case *slack.RichTextSectionUserElement:
			value = "<@" + e.UserID + ">"
			style = e.Style
		case *slack.RichTextSectionChannelElement:
			value = "<#" + e.ChannelID + ">"
			style = e.Style
		case *slack.RichTextSectionUserGroupElement:
			value = "<!subteam^" + e.UsergroupID + ">"
		case *slack.RichTextSectionTeamElement:
			value = e.TeamID
			style = e.Style
		case *slack.RichTextSectionEmojiElement:
			value = ":" + e.Name + ":"
			style = e.Style
			if e.SkinTone > 0 {
				value += fmt.Sprintf(":skin-tone-%d:", e.SkinTone)
			}
		case *slack.RichTextSectionLinkElement:
			value = "<" + e.URL
			if e.Text != "" {
				value += "|" + e.Text
			}
			value += ">"
			style = e.Style
		case *slack.RichTextSectionBroadcastElement:
			value = "<!" + e.Range + ">"
		case *slack.RichTextSectionColorElement:
			value = e.Value
		case *slack.RichTextSectionDateElement:
			value = fmt.Sprintf("<!date^%d^%s", e.Timestamp, e.Format)
			if e.URL != nil {
				value += "^" + *e.URL
			}
			if e.Fallback != nil {
				value += "|" + *e.Fallback
			}
			value += ">"
		case *slack.RichTextSectionUnknownElement:
			value = e.Raw
		}
		parts = append(parts, messageRichStyle(value, style))
	}
	return strings.Join(parts, "")
}
