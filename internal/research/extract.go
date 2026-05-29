package research

import (
	"context"
	"html"
	"regexp"
	"strings"
)

type SimpleHTMLExtractor struct{}

var (
	titleRe       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	tagRe         = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe       = regexp.MustCompile(`\s+`)
	noiseBlockRes = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`),
		regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`),
		regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`),
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
	text = strings.ReplaceAll(text, "</h1>", "\n")
	text = strings.ReplaceAll(text, "</h2>", "\n")
	text = strings.ReplaceAll(text, "</h3>", "\n")
	text = strings.ReplaceAll(text, "</p>", "\n")
	text = strings.ReplaceAll(text, "</li>", "\n")
	text = tagRe.ReplaceAllString(text, " ")
	text = normalizeText(text)
	return ExtractedDocument{Title: title, Text: text}, nil
}

func normalizeText(text string) string {
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = spaceRe.ReplaceAllString(text, " ")
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
