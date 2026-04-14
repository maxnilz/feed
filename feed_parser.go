package main

import (
	"bytes"
	"io"
	"strings"

	"github.com/mmcdole/gofeed"
)

// parseFeedWithSanitization removes XML-illegal control chars before parsing.
func parseFeedWithSanitization(fp *gofeed.Parser, r io.Reader) (*gofeed.Feed, int, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}

	cleaned, removed := sanitizeXMLControlChars(string(body))
	feed, err := fp.Parse(bytes.NewReader([]byte(cleaned)))
	if err != nil {
		return nil, removed, err
	}
	return feed, removed, nil
}

func sanitizeXMLControlChars(s string) (string, int) {
	var b strings.Builder
	b.Grow(len(s))
	removed := 0

	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			b.WriteRune(r)
			continue
		}
		removed++
	}

	return b.String(), removed
}
