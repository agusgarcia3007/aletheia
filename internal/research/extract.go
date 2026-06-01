package research

import (
	"context"
	"html"
	"regexp"
	"strings"
)

type SimpleHTMLExtractor struct{}

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	tagRe   = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe = regexp.MustCompile(`\s+`)
	// blockBreakRe marks the end of a block-level element (and <br>) so document
	// structure survives as newlines instead of being flattened into one run.
	blockBreakRe = regexp.MustCompile(`(?is)</(p|div|section|article|h[1-6]|li|ul|ol|tr|table|thead|tbody|blockquote|pre|dd|dt|figcaption)>|<br\s*/?>`)
	// inlineSpaceRe collapses runs of whitespace that are NOT newlines, so spaces
	// within a line shrink to one while paragraph/line breaks are preserved.
	inlineSpaceRe = regexp.MustCompile(`[^\S\n]+`)
	blankRunRe    = regexp.MustCompile(`\n{2,}`)
	noiseBlockRes = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`),
		regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`),
		regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`),
		regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`),
		regexp.MustCompile(`(?is)<form[^>]*>.*?</form>`),
		regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`),
		regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg>`),
	}
)

func (SimpleHTMLExtractor) Extract(_ context.Context, page FetchedPage) (ExtractedDocument, error) {
	raw := string(page.Body)
	title := ""
	if match := titleRe.FindStringSubmatch(raw); len(match) == 2 {
		title = normalizeText(match[1])
	}
	text := raw
	for _, re := range noiseBlockRes {
		text = re.ReplaceAllString(text, " ")
	}
	// Turn block boundaries into newlines BEFORE stripping inline tags, so
	// paragraphs/headings/list items stay separated (markdown-ish structure).
	text = blockBreakRe.ReplaceAllString(text, "\n")
	text = tagRe.ReplaceAllString(text, " ")
	text = normalizeStructuredText(text)
	return ExtractedDocument{Title: title, Text: text}, nil
}

// normalizeText flattens to a single clean line \u2014 used for titles.
func normalizeText(text string) string {
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = spaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// normalizeStructuredText cleans whitespace WITHIN lines but preserves the line
// breaks that carry document structure, so downstream sentence splitting sees
// real paragraphs and headings instead of one giant run-on.
func normalizeStructuredText(text string) string {
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = inlineSpaceRe.ReplaceAllString(text, " ")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	text = strings.Join(lines, "\n")
	text = blankRunRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func ChunkText(text string, size int, overlap int) []string {
	if size <= 0 {
		size = 1600
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	step := size - overlap
	var chunks []string
	for start := 0; start < len(runes); {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
		start += step
	}
	return chunks
}
