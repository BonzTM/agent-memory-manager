package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TemporalRange represents a parsed time window extracted from a query string.
type TemporalRange struct {
	After  time.Time
	Before time.Time
}

// TemporalExtraction holds the result of parsing temporal references from a query.
type TemporalExtraction struct {
	Range        *TemporalRange
	StrippedQuery string // query with temporal phrase removed
}

// month name lookup (case-insensitive).
var monthNames = map[string]time.Month{
	"january":   time.January,
	"february":  time.February,
	"march":     time.March,
	"april":     time.April,
	"may":       time.May,
	"june":      time.June,
	"july":      time.July,
	"august":    time.August,
	"september": time.September,
	"october":   time.October,
	"november":  time.November,
	"december":  time.December,
	"jan":       time.January,
	"feb":       time.February,
	"mar":       time.March,
	"apr":       time.April,
	"jun":       time.June,
	"jul":       time.July,
	"aug":       time.August,
	"sep":       time.September,
	"oct":       time.October,
	"nov":       time.November,
	"dec":       time.December,
}

// monthPattern matches month names (full and abbreviated).
var monthPattern = `(?i)(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sep|oct|nov|dec)`

// Temporal extraction patterns ordered by specificity (most specific first).
// Each pattern returns a named match group and a function to compute the range.
type temporalRule struct {
	re      *regexp.Regexp
	resolve func(match []string, now time.Time) *TemporalRange
}

var temporalRules []temporalRule

func init() {
	temporalRules = []temporalRule{
		// "earlier today"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?earlier\s+today\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				return &TemporalRange{
					After:  startOfDay(now),
					Before: now,
				}
			},
		},
		// "N days ago"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?(\d+)\s+days?\s+ago\b`),
			resolve: func(m []string, now time.Time) *TemporalRange {
				n, _ := strconv.Atoi(m[1])
				day := now.AddDate(0, 0, -n)
				return &TemporalRange{
					After:  startOfDay(day),
					Before: endOfDay(day),
				}
			},
		},
		// "N weeks ago"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?(\d+)\s+weeks?\s+ago\b`),
			resolve: func(m []string, now time.Time) *TemporalRange {
				n, _ := strconv.Atoi(m[1])
				target := now.AddDate(0, 0, -7*n)
				monday := startOfWeek(target)
				return &TemporalRange{
					After:  monday,
					Before: endOfDay(monday.AddDate(0, 0, 6)),
				}
			},
		},
		// "in Q1/Q2/Q3/Q4 2025"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?in\s+Q([1-4])\s+(\d{4})\b`),
			resolve: func(m []string, _ time.Time) *TemporalRange {
				q, _ := strconv.Atoi(m[1])
				year, _ := strconv.Atoi(m[2])
				return quarterRange(q, year)
			},
		},
		// "in Q1/Q2/Q3/Q4" (current year)
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?in\s+Q([1-4])\b`),
			resolve: func(m []string, now time.Time) *TemporalRange {
				q, _ := strconv.Atoi(m[1])
				return quarterRange(q, now.Year())
			},
		},
		// "in <month> <year>"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?in\s+` + monthPattern + `\s+(\d{4})\b`),
			resolve: func(m []string, _ time.Time) *TemporalRange {
				month := parseMonth(m[1])
				year, _ := strconv.Atoi(m[2])
				return monthRange(month, year)
			},
		},
		// "last <month>"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?last\s+` + monthPattern + `\b`),
			resolve: func(m []string, now time.Time) *TemporalRange {
				month := parseMonth(m[1])
				year := resolveLastMonth(month, now)
				return monthRange(month, year)
			},
		},
		// "in <month>" (no year — current or most recent)
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?in\s+` + monthPattern + `\b`),
			resolve: func(m []string, now time.Time) *TemporalRange {
				month := parseMonth(m[1])
				year := resolveInMonth(month, now)
				return monthRange(month, year)
			},
		},
		// "today"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?today\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				return &TemporalRange{
					After:  startOfDay(now),
					Before: now,
				}
			},
		},
		// "yesterday"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?yesterday\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				yesterday := now.AddDate(0, 0, -1)
				return &TemporalRange{
					After:  startOfDay(yesterday),
					Before: endOfDay(yesterday),
				}
			},
		},
		// "last week"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?last\s+week\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				lastWeek := now.AddDate(0, 0, -7)
				monday := startOfWeek(lastWeek)
				return &TemporalRange{
					After:  monday,
					Before: endOfDay(monday.AddDate(0, 0, 6)),
				}
			},
		},
		// "this week"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?this\s+week\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				monday := startOfWeek(now)
				return &TemporalRange{
					After:  monday,
					Before: now,
				}
			},
		},
		// "last month"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?last\s+month\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				firstOfCurrent := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
				lastMonth := firstOfCurrent.AddDate(0, -1, 0)
				return monthRange(lastMonth.Month(), lastMonth.Year())
			},
		},
		// "this month"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?this\s+month\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				return &TemporalRange{
					After:  time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()),
					Before: now,
				}
			},
		},
		// "last year"
		{
			re: regexp.MustCompile(`(?i)\b(?:from\s+)?last\s+year\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				year := now.Year() - 1
				return &TemporalRange{
					After:  time.Date(year, time.January, 1, 0, 0, 0, 0, now.Location()),
					Before: endOfDay(time.Date(year, time.December, 31, 0, 0, 0, 0, now.Location())),
				}
			},
		},
		// "earlier" / "previously" / "recently" — last 3 days
		{
			re: regexp.MustCompile(`(?i)\b(earlier|previously|recently)\b`),
			resolve: func(_ []string, now time.Time) *TemporalRange {
				return &TemporalRange{
					After:  startOfDay(now.AddDate(0, 0, -3)),
					Before: now,
				}
			},
		},
	}
}

// ExtractTemporal parses temporal references from a query string and returns
// the extracted time range plus the query with the temporal phrase stripped.
// If no temporal reference is found, returns nil range and the original query.
func ExtractTemporal(query string, now time.Time) TemporalExtraction {
	for _, rule := range temporalRules {
		loc := rule.re.FindStringSubmatchIndex(query)
		if loc == nil {
			continue
		}
		// Extract named subgroups.
		match := make([]string, rule.re.NumSubexp()+1)
		for i := range match {
			if loc[2*i] >= 0 {
				match[i] = query[loc[2*i]:loc[2*i+1]]
			}
		}
		tr := rule.resolve(match, now)
		if tr == nil {
			continue
		}
		// Strip temporal phrase from query.
		stripped := query[:loc[0]] + query[loc[1]:]
		stripped = cleanStrippedQuery(stripped)
		return TemporalExtraction{
			Range:         tr,
			StrippedQuery: stripped,
		}
	}
	return TemporalExtraction{
		Range:         nil,
		StrippedQuery: query,
	}
}

// --- helpers ---

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
}

func startOfWeek(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7 for Monday-based weeks
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return startOfDay(monday)
}

func parseMonth(s string) time.Month {
	return monthNames[strings.ToLower(s)]
}

// resolveLastMonth returns the year for "last <month>". If the named month
// is the current month, return previous year. Otherwise return the most
// recent past occurrence.
func resolveLastMonth(month time.Month, now time.Time) int {
	if now.Month() == month {
		return now.Year() - 1
	}
	if month < now.Month() {
		return now.Year()
	}
	return now.Year() - 1
}

// resolveInMonth returns the year for "in <month>". If the named month is
// the current month, return current year. Otherwise return the most recent
// past occurrence.
func resolveInMonth(month time.Month, now time.Time) int {
	if month <= now.Month() {
		return now.Year()
	}
	return now.Year() - 1
}

func monthRange(month time.Month, year int) *TemporalRange {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	return &TemporalRange{
		After:  first,
		Before: endOfDay(last),
	}
}

func quarterRange(q int, year int) *TemporalRange {
	startMonth := time.Month((q-1)*3 + 1)
	first := time.Date(year, startMonth, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 3, -1)
	return &TemporalRange{
		After:  first,
		Before: endOfDay(last),
	}
}

// cleanStrippedQuery removes leftover prepositions/connectors and normalizes whitespace.
func cleanStrippedQuery(s string) string {
	s = strings.TrimSpace(s)
	// Remove leading/trailing prepositions left after stripping.
	s = regexp.MustCompile(`(?i)^(from|in|during|on|since)\s+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)\s+(from|in|during|on|since)$`).ReplaceAllString(s, "")
	// Collapse whitespace.
	s = regexp.MustCompile(`\s{2,}`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
