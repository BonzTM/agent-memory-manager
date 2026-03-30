package service_test

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func testConcreteService(t *testing.T) (*service.AMMService, *sqlite.SQLiteRepository) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := service.New(repo, dbPath, nil, nil)
	t.Cleanup(func() { _ = db.Close() })
	return svc, repo
}

func seedEvents(t *testing.T, svc *service.AMMService, sessionID string, count int) []core.Event {
	t.Helper()
	ctx := context.Background()
	seeded := make([]core.Event, 0, count)
	for i := 0; i < count; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message_user",
			SourceSystem: "test",
			SessionID:    sessionID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("event-%02d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("IngestEvent %d: %v", i, err)
		}
		seeded = append(seeded, *evt)
	}
	return seeded
}

func seedSummary(t *testing.T, repo *sqlite.SQLiteRepository, sessionID string, depth int, body string, eventIDs []string) core.Summary {
	t.Helper()
	now := time.Now().UTC()
	summary := core.Summary{
		ID:               fmt.Sprintf("sum_%d", now.UnixNano()),
		Kind:             "leaf",
		Depth:            depth,
		Scope:            core.ScopeSession,
		SessionID:        sessionID,
		Body:             body,
		TightDescription: body,
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{EventIDs: eventIDs},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(context.Background(), &summary); err != nil {
		t.Fatalf("InsertSummary: %v", err)
	}
	return summary
}

func TestFormatContextWindow_EmptySession(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_empty"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	if result.Content != "" {
		t.Fatalf("expected empty content, got %q", result.Content)
	}
	if result.SummaryCount != 0 || result.FreshCount != 0 || result.EstTokens != 0 {
		t.Fatalf("expected all counts 0, got summary=%d fresh=%d est=%d", result.SummaryCount, result.FreshCount, result.EstTokens)
	}
	if len(result.Manifest) != 0 {
		t.Fatalf("expected empty manifest, got %d entries", len(result.Manifest))
	}
}

func TestFormatContextWindow_FreshEventsOnly(t *testing.T) {
	svc, _ := testConcreteService(t)
	ctx := context.Background()
	seedEvents(t, svc, "sess_fresh_only", 5)

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_fresh_only"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	if result.SummaryCount != 0 {
		t.Fatalf("expected 0 summaries, got %d", result.SummaryCount)
	}
	if result.FreshCount != 5 {
		t.Fatalf("expected 5 fresh events, got %d", result.FreshCount)
	}
	if strings.Count(result.Content, "<event ") != 5 {
		t.Fatalf("expected 5 <event> entries, got content=%q", result.Content)
	}
}

func TestFormatContextWindow_FreshTailIsNewest(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	events := seedEvents(t, svc, "sess_fresh_newest", 10)

	olderEventIDs := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		olderEventIDs = append(olderEventIDs, events[i].ID)
	}
	seedSummary(t, repo, "sess_fresh_newest", 1, "summary for older events", olderEventIDs)

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_fresh_newest", FreshTailCount: 3})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}

	freshSection := result.Content
	if idx := strings.LastIndex(result.Content, "</summary>"); idx >= 0 {
		freshSection = result.Content[idx+len("</summary>"):]
	}

	for _, want := range []string{"event-07", "event-08", "event-09"} {
		if !strings.Contains(freshSection, want) {
			t.Fatalf("expected freshest event %q in fresh section, content=%q", want, freshSection)
		}
	}
	for _, notWant := range []string{"event-00", "event-01", "event-02", "event-03", "event-04", "event-05", "event-06"} {
		if strings.Contains(freshSection, notWant) {
			t.Fatalf("did not expect older event %q in fresh section, content=%q", notWant, freshSection)
		}
	}
}

func TestFormatContextWindow_ChronologicalOrder(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	events := seedEvents(t, svc, "sess_chrono", 10)

	olderEventIDs := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		olderEventIDs = append(olderEventIDs, events[i].ID)
	}
	seedSummary(t, repo, "sess_chrono", 1, "summary for first seven", olderEventIDs)

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_chrono", FreshTailCount: 3})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}

	summaryIdx := strings.Index(result.Content, "<summary ")
	if summaryIdx < 0 {
		t.Fatalf("expected summary in output, got %q", result.Content)
	}
	firstEventIdx := strings.Index(result.Content, "<event ")
	if firstEventIdx < 0 {
		t.Fatalf("expected events in output, got %q", result.Content)
	}
	if summaryIdx > firstEventIdx {
		t.Fatalf("expected summaries before fresh events, got %q", result.Content)
	}

	idx07 := strings.Index(result.Content, "event-07")
	idx08 := strings.Index(result.Content, "event-08")
	idx09 := strings.Index(result.Content, "event-09")
	if idx07 < 0 || idx08 < 0 || idx09 < 0 {
		t.Fatalf("expected fresh events 07..09 in output, got %q", result.Content)
	}
	if !(idx07 < idx08 && idx08 < idx09) {
		t.Fatalf("expected fresh events oldest-to-newest, got %q", result.Content)
	}
	if strings.Count(result.Content, "<event ") != 3 {
		t.Fatalf("expected exactly 3 fresh events, got content=%q", result.Content)
	}
}

func TestFormatContextWindow_SummariesAndFresh(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	events := seedEvents(t, svc, "sess_mix", 40)
	coveredIDs := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		coveredIDs = append(coveredIDs, events[i].ID)
	}
	seedSummary(t, repo, "sess_mix", 1, "summary covering first thirty", coveredIDs)

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_mix"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	if result.SummaryCount == 0 {
		t.Fatalf("expected summaries to be included")
	}
	if result.FreshCount != 32 {
		t.Fatalf("expected fresh_count=32 by default, got %d", result.FreshCount)
	}
	if strings.Count(result.Content, "<summary ") == 0 {
		t.Fatalf("expected summary content, got %q", result.Content)
	}
}

func TestFormatContextWindow_ManifestComplete(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	events := seedEvents(t, svc, "sess_manifest", 6)
	seedSummary(t, repo, "sess_manifest", 1, "manifest summary", []string{events[0].ID, events[1].ID})

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_manifest"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	if len(result.Manifest) != result.SummaryCount+result.FreshCount {
		t.Fatalf("manifest size mismatch: got=%d want=%d", len(result.Manifest), result.SummaryCount+result.FreshCount)
	}
	for _, entry := range result.Manifest {
		if entry.ID == "" || entry.Kind == "" || entry.StableRef == "" {
			t.Fatalf("manifest entry incomplete: %+v", entry)
		}
	}
}

func TestFormatContextWindow_StableIDs(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	events := seedEvents(t, svc, "sess_stable", 3)
	seedSummary(t, repo, "sess_stable", 1, "stable summary", []string{events[0].ID})

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_stable"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	for _, entry := range result.Manifest {
		switch entry.Kind {
		case "summary":
			if !strings.HasPrefix(entry.StableRef, "summary:") {
				t.Fatalf("expected summary stable ref, got %q", entry.StableRef)
			}
		case "event":
			if !strings.HasPrefix(entry.StableRef, "event:") {
				t.Fatalf("expected event stable ref, got %q", entry.StableRef)
			}
		default:
			t.Fatalf("unexpected manifest kind %q", entry.Kind)
		}
	}
}

func TestFormatContextWindow_TokenEstimate(t *testing.T) {
	svc, _ := testConcreteService(t)
	ctx := context.Background()
	seedEvents(t, svc, "sess_tokens", 4)

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: "sess_tokens"})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}
	expected := len(result.Content) / 4
	if diff := result.EstTokens - expected; diff < -1 || diff > 1 {
		t.Fatalf("expected est tokens near len(content)/4; got=%d expected=%d", result.EstTokens, expected)
	}
}

func TestFormatContextWindow_EscapesSummaryAndEventContent(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	sessionID := "sess_escape"

	eventRaw := `event <alpha>&beta`
	event, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message_user",
		SourceSystem: "test",
		SessionID:    sessionID,
		PrivacyLevel: core.PrivacyPrivate,
		Content:      eventRaw,
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}

	summaryRaw := `summary <tag>&value`
	seedSummary(t, repo, sessionID, 1, summaryRaw, []string{event.ID})

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: sessionID})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}

	if !strings.Contains(result.Content, "summary &lt;tag&gt;&amp;value") {
		t.Fatalf("expected escaped summary body, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "event &lt;alpha&gt;&amp;beta") {
		t.Fatalf("expected escaped event content, got %q", result.Content)
	}
}

func TestFormatContextWindow_EscapesEventAttributeValues(t *testing.T) {
	svc, _ := testConcreteService(t)
	ctx := context.Background()
	sessionID := "sess_escape_attrs"
	rawKind := `message_user" injected="x<y`

	if _, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         rawKind,
		SourceSystem: "test",
		SessionID:    sessionID,
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "attribute escaping",
		OccurredAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: sessionID})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}

	wrapped := "<root>" + result.Content + "</root>"
	decoder := xml.NewDecoder(strings.NewReader(wrapped))

	eventCount := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode xml token: %v", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "event" {
			continue
		}
		eventCount++

		attrs := map[string]string{}
		for _, attr := range start.Attr {
			attrs[attr.Name.Local] = attr.Value
		}
		if len(attrs) != 2 {
			t.Fatalf("expected exactly 2 event attributes, got %d (%v)", len(attrs), attrs)
		}
		if attrs["kind"] != rawKind {
			t.Fatalf("expected kind attribute to round-trip, got %q want %q", attrs["kind"], rawKind)
		}
		if attrs["id"] == "" {
			t.Fatalf("expected id attribute to be present, attrs=%v", attrs)
		}
	}

	if eventCount != 1 {
		t.Fatalf("expected exactly one event, got %d", eventCount)
	}
}

func TestFormatContextWindow_TiedTimestampsUseSequenceOrdering(t *testing.T) {
	svc, repo := testConcreteService(t)
	ctx := context.Background()
	sessionID := "sess_tied_sequence"
	now := time.Now().UTC().Truncate(time.Second)

	first := &core.Event{
		ID:           "z_evt_tied_seq",
		Kind:         "message_user",
		SourceSystem: "test",
		SessionID:    sessionID,
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "first inserted",
		OccurredAt:   now,
		IngestedAt:   now,
	}
	if err := repo.InsertEvent(ctx, first); err != nil {
		t.Fatalf("InsertEvent first: %v", err)
	}

	second := &core.Event{
		ID:           "a_evt_tied_seq",
		Kind:         "message_user",
		SourceSystem: "test",
		SessionID:    sessionID,
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "second inserted",
		OccurredAt:   now,
		IngestedAt:   now,
	}
	if err := repo.InsertEvent(ctx, second); err != nil {
		t.Fatalf("InsertEvent second: %v", err)
	}

	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: sessionID, FreshTailCount: 2})
	if err != nil {
		t.Fatalf("FormatContextWindow: %v", err)
	}

	firstIdx := strings.Index(result.Content, "first inserted")
	secondIdx := strings.Index(result.Content, "second inserted")
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("expected both events in output, got %q", result.Content)
	}
	if firstIdx >= secondIdx {
		t.Fatalf("expected sequence ordering for tied timestamps, got %q", result.Content)
	}
}
