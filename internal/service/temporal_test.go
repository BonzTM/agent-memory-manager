package service

import (
	"testing"
	"time"
)

// reference time: Wednesday, April 2, 2026, 14:30:00 UTC
var refTime = time.Date(2026, time.April, 2, 14, 30, 0, 0, time.UTC)

func TestExtractTemporal_Today(t *testing.T) {
	ext := ExtractTemporal("errors from today", refTime)
	assertRange(t, ext, "2026-04-02", "2026-04-02")
	assertEqual(t, "stripped", "errors", ext.StrippedQuery)
}

func TestExtractTemporal_Yesterday(t *testing.T) {
	ext := ExtractTemporal("what we did yesterday", refTime)
	assertRange(t, ext, "2026-04-01", "2026-04-01")
	assertEqual(t, "stripped", "what we did", ext.StrippedQuery)
}

func TestExtractTemporal_EarlierToday(t *testing.T) {
	ext := ExtractTemporal("session recall from earlier today", refTime)
	assertRange(t, ext, "2026-04-02", "2026-04-02")
	assertEqual(t, "stripped", "session recall", ext.StrippedQuery)
}

func TestExtractTemporal_LastWeek(t *testing.T) {
	ext := ExtractTemporal("session recall work from last week", refTime)
	assertRange(t, ext, "2026-03-23", "2026-03-29")
	assertEqual(t, "stripped", "session recall work", ext.StrippedQuery)
}

func TestExtractTemporal_ThisWeek(t *testing.T) {
	ext := ExtractTemporal("work done this week", refTime)
	assertRange(t, ext, "2026-03-30", "2026-04-02")
	assertEqual(t, "stripped", "work done", ext.StrippedQuery)
}

func TestExtractTemporal_NDaysAgo(t *testing.T) {
	ext := ExtractTemporal("issues from 3 days ago", refTime)
	assertRange(t, ext, "2026-03-30", "2026-03-30")
	assertEqual(t, "stripped", "issues", ext.StrippedQuery)
}

func TestExtractTemporal_NWeeksAgo(t *testing.T) {
	ext := ExtractTemporal("work from 2 weeks ago", refTime)
	// 2 weeks ago from April 2 = March 19 (Thursday), week starts March 16 (Monday)
	assertRange(t, ext, "2026-03-16", "2026-03-22")
	assertEqual(t, "stripped", "work", ext.StrippedQuery)
}

func TestExtractTemporal_LastMonth(t *testing.T) {
	ext := ExtractTemporal("bugs from last month", refTime)
	assertRange(t, ext, "2026-03-01", "2026-03-31")
	assertEqual(t, "stripped", "bugs", ext.StrippedQuery)
}

func TestExtractTemporal_ThisMonth(t *testing.T) {
	ext := ExtractTemporal("PRs this month", refTime)
	assertRange(t, ext, "2026-04-01", "2026-04-02")
	assertEqual(t, "stripped", "PRs", ext.StrippedQuery)
}

func TestExtractTemporal_LastNamedMonth_PastMonth(t *testing.T) {
	// "last March" in April 2026 = March 2026
	ext := ExtractTemporal("work from last March", refTime)
	assertRange(t, ext, "2026-03-01", "2026-03-31")
	assertEqual(t, "stripped", "work", ext.StrippedQuery)
}

func TestExtractTemporal_LastNamedMonth_CurrentMonth(t *testing.T) {
	// "last April" in April 2026 = April 2025
	ext := ExtractTemporal("sessions from last April", refTime)
	assertRange(t, ext, "2025-04-01", "2025-04-30")
	assertEqual(t, "stripped", "sessions", ext.StrippedQuery)
}

func TestExtractTemporal_LastNamedMonth_FutureMonth(t *testing.T) {
	// "last September" in April 2026 = September 2025
	ext := ExtractTemporal("work from last September", refTime)
	assertRange(t, ext, "2025-09-01", "2025-09-30")
	assertEqual(t, "stripped", "work", ext.StrippedQuery)
}

func TestExtractTemporal_InMonth(t *testing.T) {
	// "in March" in April 2026 = March 2026 (most recent past)
	ext := ExtractTemporal("consolidation work in March", refTime)
	assertRange(t, ext, "2026-03-01", "2026-03-31")
	assertEqual(t, "stripped", "consolidation work", ext.StrippedQuery)
}

func TestExtractTemporal_InMonthCurrentMonth(t *testing.T) {
	// "in April" in April 2026 = current month
	ext := ExtractTemporal("sessions in April", refTime)
	assertRange(t, ext, "2026-04-01", "2026-04-30")
	assertEqual(t, "stripped", "sessions", ext.StrippedQuery)
}

func TestExtractTemporal_InMonthFuture(t *testing.T) {
	// "in September" in April 2026 = September 2025 (most recent past)
	ext := ExtractTemporal("work in September", refTime)
	assertRange(t, ext, "2025-09-01", "2025-09-30")
	assertEqual(t, "stripped", "work", ext.StrippedQuery)
}

func TestExtractTemporal_InMonthYear(t *testing.T) {
	ext := ExtractTemporal("implemented in September 2025", refTime)
	assertRange(t, ext, "2025-09-01", "2025-09-30")
	assertEqual(t, "stripped", "implemented", ext.StrippedQuery)
}

func TestExtractTemporal_InQuarter(t *testing.T) {
	ext := ExtractTemporal("Q1 work in Q1", refTime)
	assertRange(t, ext, "2026-01-01", "2026-03-31")
	assertEqual(t, "stripped", "Q1 work", ext.StrippedQuery)
}

func TestExtractTemporal_InQuarterYear(t *testing.T) {
	ext := ExtractTemporal("features in Q3 2025", refTime)
	assertRange(t, ext, "2025-07-01", "2025-09-30")
	assertEqual(t, "stripped", "features", ext.StrippedQuery)
}

func TestExtractTemporal_LastYear(t *testing.T) {
	ext := ExtractTemporal("all work from last year", refTime)
	assertRange(t, ext, "2025-01-01", "2025-12-31")
	assertEqual(t, "stripped", "all work", ext.StrippedQuery)
}

func TestExtractTemporal_Earlier(t *testing.T) {
	ext := ExtractTemporal("we discussed earlier", refTime)
	assertRange(t, ext, "2026-03-30", "2026-04-02")
	assertEqual(t, "stripped", "we discussed", ext.StrippedQuery)
}

func TestExtractTemporal_Previously(t *testing.T) {
	ext := ExtractTemporal("previously we implemented X", refTime)
	assertRange(t, ext, "2026-03-30", "2026-04-02")
	assertEqual(t, "stripped", "we implemented X", ext.StrippedQuery)
}

func TestExtractTemporal_Recently(t *testing.T) {
	ext := ExtractTemporal("recently worked on auth", refTime)
	assertRange(t, ext, "2026-03-30", "2026-04-02")
	assertEqual(t, "stripped", "worked on auth", ext.StrippedQuery)
}

func TestExtractTemporal_NoMatch(t *testing.T) {
	ext := ExtractTemporal("session consolidation pipeline", refTime)
	if ext.Range != nil {
		t.Fatal("expected nil range for non-temporal query")
	}
	assertEqual(t, "stripped", "session consolidation pipeline", ext.StrippedQuery)
}

func TestExtractTemporal_EmptyQuery(t *testing.T) {
	ext := ExtractTemporal("", refTime)
	if ext.Range != nil {
		t.Fatal("expected nil range for empty query")
	}
	assertEqual(t, "stripped", "", ext.StrippedQuery)
}

func TestExtractTemporal_AbbreviatedMonth(t *testing.T) {
	ext := ExtractTemporal("work in Sep 2025", refTime)
	assertRange(t, ext, "2025-09-01", "2025-09-30")
	assertEqual(t, "stripped", "work", ext.StrippedQuery)
}

func TestExtractTemporal_LastAbbreviatedMonth(t *testing.T) {
	ext := ExtractTemporal("bugs from last Jan", refTime)
	assertRange(t, ext, "2026-01-01", "2026-01-31")
	assertEqual(t, "stripped", "bugs", ext.StrippedQuery)
}

func TestExtractTemporal_1DayAgo(t *testing.T) {
	ext := ExtractTemporal("errors from 1 day ago", refTime)
	assertRange(t, ext, "2026-04-01", "2026-04-01")
	assertEqual(t, "stripped", "errors", ext.StrippedQuery)
}

func TestExtractTemporal_1WeekAgo(t *testing.T) {
	ext := ExtractTemporal("sessions from 1 week ago", refTime)
	// 1 week ago from April 2 = March 26 (Thursday), week of March 23
	assertRange(t, ext, "2026-03-23", "2026-03-29")
	assertEqual(t, "stripped", "sessions", ext.StrippedQuery)
}

// --- helpers ---

func assertRange(t *testing.T, ext TemporalExtraction, afterDate, beforeDate string) {
	t.Helper()
	if ext.Range == nil {
		t.Fatal("expected non-nil range")
	}
	gotAfter := ext.Range.After.Format("2006-01-02")
	if gotAfter != afterDate {
		t.Errorf("After: got %s, want %s", gotAfter, afterDate)
	}
	gotBefore := ext.Range.Before.Format("2006-01-02")
	if gotBefore != beforeDate {
		t.Errorf("Before: got %s, want %s", gotBefore, beforeDate)
	}
}

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}
