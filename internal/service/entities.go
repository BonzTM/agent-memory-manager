package service

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// commonCapitalized is the set of English words that are frequently capitalized
// at sentence starts but are not entity names.
var commonCapitalized = map[string]bool{
	"The": true, "This": true, "That": true, "What": true,
	"When": true, "Where": true, "How": true, "Why": true,
	"It": true, "I": true, "A": true, "An": true,
	"If": true, "So": true, "But": true, "Or": true,
	"And": true, "Do": true, "Is": true, "Are": true,
	"Was": true, "Were": true, "Has": true, "Have": true,
	"Had": true, "Will": true, "Would": true, "Could": true,
	"Should": true, "Can": true, "May": true, "Might": true,
}

// ExtractEntities extracts potential entity names from text using capitalized-token heuristics.
func ExtractEntities(text string) []string {
	words := strings.Fields(text)

	seen := make(map[string]bool)
	var results []string

	// Walk through words, collecting runs of capitalized tokens.
	i := 0
	for i < len(words) {
		word := stripPunctuation(words[i])
		if !isCapitalized(word) || commonCapitalized[word] {
			i++
			continue
		}

		// Start of a capitalized run.
		run := []string{word}
		j := i + 1
		for j < len(words) {
			next := stripPunctuation(words[j])
			if !isCapitalized(next) || commonCapitalized[next] {
				break
			}
			run = append(run, next)
			j++
		}

		// Emit multi-word name if run length > 1.
		if len(run) > 1 {
			name := strings.Join(run, " ")
			if !seen[name] {
				seen[name] = true
				results = append(results, name)
			}
		}

		// Also emit each individual word in the run.
		for _, w := range run {
			if !seen[w] {
				seen[w] = true
				results = append(results, w)
			}
		}

		i = j
	}

	return results
}

// MatchEntityAliases checks extracted names against known entity aliases.
// It returns entity IDs where the canonical_name or any alias matches
// (case-insensitive) an extracted name.
func MatchEntityAliases(extracted []string, entities []core.Entity) []string {
	if len(extracted) == 0 || len(entities) == 0 {
		return nil
	}

	// Build a set of lower-cased extracted names for fast lookup.
	extractedLower := make(map[string]bool, len(extracted))
	for _, name := range extracted {
		extractedLower[strings.ToLower(name)] = true
	}

	seen := make(map[string]bool)
	var matched []string

	for _, ent := range entities {
		if seen[ent.ID] {
			continue
		}
		if extractedLower[strings.ToLower(ent.CanonicalName)] {
			seen[ent.ID] = true
			matched = append(matched, ent.ID)
			continue
		}
		for _, alias := range ent.Aliases {
			if extractedLower[strings.ToLower(alias)] {
				seen[ent.ID] = true
				matched = append(matched, ent.ID)
				break
			}
		}
	}

	return matched
}

// isCapitalized returns true if the word starts with an uppercase letter and
// has at least 2 characters.
func isCapitalized(word string) bool {
	if utf8.RuneCountInString(word) < 2 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(word)
	return unicode.IsUpper(r)
}

// stripPunctuation removes leading and trailing non-letter, non-digit characters.
func stripPunctuation(word string) string {
	runes := []rune(word)
	start := 0
	for start < len(runes) && !unicode.IsLetter(runes[start]) && !unicode.IsDigit(runes[start]) {
		start++
	}
	end := len(runes)
	for end > start && !unicode.IsLetter(runes[end-1]) && !unicode.IsDigit(runes[end-1]) {
		end--
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}
