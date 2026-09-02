package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// bnBlock/bnInline mirror the subset of BlockNote's default block/inline
// schema (https://www.blocknotejs.org) actually used by the host's
// Documentation feature: paragraph, heading, bullet/numbered/check list
// items, code blocks, quotes, plus inline text/link spans with bold/italic/
// strike/code marks. A skill's body now lives in a real project Document
// (edited via the host's own BlockNote editor) rather than being authored
// here, so this converts that stored block JSON into the plain markdown
// body a SKILL.md file needs when served to external agent clients.
type bnBlock struct {
	Type     string          `json:"type"`
	Props    map[string]any  `json:"props"`
	Content  json.RawMessage `json:"content"`
	Children []bnBlock       `json:"children"`
}

type bnInline struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Styles  map[string]any `json:"styles"`
	Href    string         `json:"href"`
	Content []bnInline     `json:"content"`
}

// blocksToMarkdown converts a document's stored BlockNote block array (raw
// jsonb bytes, possibly nil/empty for a still-blank document) into markdown.
// Any block type this converter doesn't recognise (e.g. tables, images)
// falls back to rendering just its inline text content, so unsupported
// content degrades gracefully instead of vanishing outright.
func blocksToMarkdown(raw []byte) string {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var blocks []bnBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	renderBlocks(&b, blocks, 0)
	return strings.TrimSpace(b.String())
}

func renderBlocks(b *strings.Builder, blocks []bnBlock, depth int) {
	indent := strings.Repeat("  ", depth)
	numberedRun := 0
	prevType := ""
	for _, blk := range blocks {
		if blk.Type == "numberedListItem" {
			if prevType != "numberedListItem" {
				numberedRun = 0
			}
			numberedRun++
		}
		prevType = blk.Type

		text := renderInline(blk.Content)
		switch blk.Type {
		case "heading":
			level := propInt(blk.Props, "level", 1)
			if level < 1 {
				level = 1
			}
			if level > 6 {
				level = 6
			}
			b.WriteString(indent + strings.Repeat("#", level) + " " + text + "\n\n")
		case "bulletListItem":
			b.WriteString(indent + "- " + text + "\n")
		case "numberedListItem":
			b.WriteString(indent + strconv.Itoa(numberedRun) + ". " + text + "\n")
		case "checkListItem":
			mark := " "
			if propBool(blk.Props, "checked") {
				mark = "x"
			}
			b.WriteString(indent + "- [" + mark + "] " + text + "\n")
		case "codeBlock":
			lang, _ := blk.Props["language"].(string)
			b.WriteString(indent + "```" + lang + "\n" + text + "\n" + indent + "```\n\n")
		case "quote":
			b.WriteString(indent + "> " + text + "\n\n")
		case "paragraph":
			if text == "" {
				b.WriteString("\n")
			} else {
				b.WriteString(indent + text + "\n\n")
			}
		default:
			if text != "" {
				b.WriteString(indent + text + "\n\n")
			}
		}
		if len(blk.Children) > 0 {
			renderBlocks(b, blk.Children, depth+1)
		}
	}
}

func renderInline(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var spans []bnInline
	if err := json.Unmarshal(raw, &spans); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(renderInlineSpan(s))
	}
	return sb.String()
}

func renderInlineSpan(s bnInline) string {
	if s.Type == "link" {
		var inner strings.Builder
		for _, c := range s.Content {
			inner.WriteString(renderInlineSpan(c))
		}
		return "[" + inner.String() + "](" + s.Href + ")"
	}

	text := s.Text
	if propBool(s.Styles, "code") {
		return "`" + text + "`"
	}
	if propBool(s.Styles, "bold") {
		text = "**" + text + "**"
	}
	if propBool(s.Styles, "italic") {
		text = "_" + text + "_"
	}
	if propBool(s.Styles, "strike") {
		text = "~~" + text + "~~"
	}
	return text
}

func propInt(props map[string]any, key string, def int) int {
	switch n := props[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

func propBool(props map[string]any, key string) bool {
	b, _ := props[key].(bool)
	return b
}
