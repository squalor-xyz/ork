// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"strings"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

const nearDuplicateMin = 0.5

// NearDuplicateTitles returns similar, unfinished items in list order. Titles
// are compared as sets of ASCII content words using the Jaccard index.
func NearDuplicateTitles(title, selfID string, open []slicercli.Item, min float64) []string {
	words := titleWords(title)
	var ids []string
	for _, item := range open {
		if item.ID == selfID || item.Status == "done" || item.Status == "retired" {
			continue
		}
		other := titleWords(item.Title)
		intersection := 0
		for word := range words {
			if other[word] {
				intersection++
			}
		}
		union := len(words) + len(other) - intersection
		if union > 0 && float64(intersection)/float64(union) >= min {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func titleWords(title string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(word) < 2 {
			continue
		}
		switch word {
		case "an", "and", "are", "as", "at", "be", "because", "both", "by", "can", "for", "from", "in", "into", "is", "it", "its", "may", "no", "not", "of", "on", "or", "so", "such", "that", "the", "then", "this", "to", "when", "whether", "with", "your":
			continue
		}
		words[word] = true
	}
	return words
}
