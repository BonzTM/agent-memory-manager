package service

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// knownTechEntities is the set of well-known technology names that commonly
// appear in lowercase but should still be extracted as entities.
var knownTechEntities = map[string]bool{
	"redis": true, "postgres": true, "postgresql": true, "mysql": true,
	"sqlite": true, "mongodb": true, "dynamodb": true, "elasticsearch": true,
	"kubernetes": true, "docker": true, "nginx": true, "apache": true,
	"grafana": true, "prometheus": true, "terraform": true, "ansible": true,
	"jenkins": true, "circleci": true, "traefik": true, "consul": true,
	"vault": true, "nomad": true, "kafka": true, "rabbitmq": true,
	"nats": true, "grpc": true, "graphql": true, "webpack": true,
	"vite": true, "eslint": true, "prettier": true, "jest": true,
	"mocha": true, "pytest": true, "golang": true, "rustc": true,
	"llvm": true, "clang": true, "gcc": true, "cmake": true,
	"bazel": true, "gradle": true, "maven": true, "npm": true,
	"yarn": true, "pnpm": true, "pip": true, "cargo": true,
	"helm": true, "istio": true, "envoy": true, "etcd": true,
	"zookeeper": true, "minio": true, "ceph": true, "clickhouse": true,
	"cockroachdb": true, "timescaledb": true, "influxdb": true,
	"supabase": true, "firebase": true, "vercel": true, "netlify": true,
	"datadog": true, "sentry": true, "pagerduty": true, "opsgenie": true,
}

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

// ExtractEntities extracts likely entity names from text using capitalized-token
// heuristics and a known technology entity list for lowercase matching.
func ExtractEntities(text string) []string {
	words := strings.Fields(text)

	seen := make(map[string]bool)
	var results []string

	// Walk through words, collecting runs of capitalized tokens
	// and matching known lowercase tech entities.
	i := 0
	for i < len(words) {
		word := stripPunctuation(words[i])

		// Check for known lowercase tech entities.
		if wordLower := strings.ToLower(word); knownTechEntities[wordLower] {
			if !seen[wordLower] {
				seen[wordLower] = true
				results = append(results, wordLower)
			}
			i++
			continue
		}

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

// MatchEntityAliases returns entity IDs whose canonical name or aliases match
// any extracted name case-insensitively.
func MatchEntityAliases(extracted []string, entities []core.Entity) []string {
	if len(extracted) == 0 || len(entities) == 0 {
		return nil
	}

	// Build a set of lower-cased extracted names for fast lookup.
	extractedLower := make(map[string]bool, len(extracted))
	for _, name := range extracted {
		normalized := normalizeEntityTerm(name)
		if normalized == "" {
			continue
		}
		extractedLower[normalized] = true
	}

	seen := make(map[string]bool)
	var matched []string

	for _, ent := range entities {
		if seen[ent.ID] {
			continue
		}
		for normalized := range extractedLower {
			if entityMatchesTerm(&ent, normalized) {
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
