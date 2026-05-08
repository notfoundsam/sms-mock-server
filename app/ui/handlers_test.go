package ui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
	"github.com/notfoundsam/sms-mock-server/app/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHandler builds a Handler against the real templates directory
// and a FakeStore. Use mutate to seed the store before tests.
func newTestHandler(t *testing.T, mutate ...func(*testutil.FakeStore)) (http.Handler, *testutil.FakeStore) {
	t.Helper()
	store := testutil.NewFakeStore()
	for _, fn := range mutate {
		fn(store)
	}

	engine, err := tmpl.New(os.DirFS("../templates"), tmpl.Options{
		Provider: "twilio",
		Now:      func() time.Time { return time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC) },
	})
	require.NoError(t, err, "template engine")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(logger, store, engine, "twilio", "UTC")

	mux := http.NewServeMux()
	h.Register(mux)
	return mux, store
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	h.ServeHTTP(rec, req)
	return rec
}

// --- pages ---

func TestDashboard_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/")
	require.Equal(t, 200, rec.Code, "body=%s", rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "<title>Dashboard - SMS Mock Server</title>", "title missing or wrong; body excerpt: %s", body[:min(300, len(body))])
	assert.Contains(t, body, "Total Messages", "stats cards missing")
	assert.Contains(t, body, "Total Calls", "stats cards missing")
	assert.Contains(t, body, "No messages yet", "empty messages state missing")
	assert.Contains(t, body, "Provider: TWILIO", "provider header missing/wrong")
}

func TestDashboard_WithData(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{
			SID: "SM_dashboard_test_x", Provider: "twilio",
			From: "+15550000001", To: "+15551234567", Body: "hello", Status: "delivered",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
		_ = s.SaveCall(ctx, &storage.Call{
			SID: "CA_dashboard_test_x", Provider: "twilio",
			From: "+15550000001", To: "+15551234567", Status: "completed",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	})
	rec := get(t, h, "/")
	body := rec.Body.String()

	// Stats reflect counts
	assert.Contains(t, body, ">1<", "expected stats values to include 1")
	// Recent message rendered. html/template escapes "+" → "&#43;".
	assert.Contains(t, body, "&#43;15550000001", "phone numbers not rendered (HTML-escaped form expected)")
	assert.Contains(t, body, "&#43;15551234567", "phone numbers not rendered (HTML-escaped form expected)")
	assert.Contains(t, body, "delivered", "status not rendered")
}

func TestDashboard_NotFoundForNonRootPath(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/some-other-path")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMessagesPage_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/messages")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "All Messages", "page heading missing")
	assert.Contains(t, body, "No messages found", "empty state missing")
}

func TestMessagesPage_Pagination(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		// 51 messages → 2 pages. SIDs need to be unique or FakeStore drops them.
		for i := 0; i < 51; i++ {
			_ = s.SaveMessage(ctx, &storage.Message{
				SID: "SM" + strconv.Itoa(i) + strings.Repeat("x", 28),
				Provider: "twilio", From: "+1", To: "+2",
				Body: "msg", Status: "queued",
			})
		}
	})

	// page 1
	rec := get(t, h, "/ui/messages?page=1")
	body := rec.Body.String()
	assert.Contains(t, body, "Page 1 of 2", "page 1 indicator missing; body excerpt: %s", body[max(0, len(body)-500):])
	assert.Contains(t, body, `href="?page=2"`, "Next link to page 2 missing on page 1")

	// page 2
	rec = get(t, h, "/ui/messages?page=2")
	body = rec.Body.String()
	assert.Contains(t, body, "Page 2 of 2", "page 2 indicator missing")
	assert.Contains(t, body, `href="?page=1"`, "Previous link to page 1 missing on page 2")
}

func TestMessagesPage_InvalidPageClampsToOne(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []string{"/ui/messages?page=0", "/ui/messages?page=-5", "/ui/messages?page=garbage"}
	for _, p := range cases {
		rec := get(t, h, p)
		assert.Equal(t, 200, rec.Code, "%s: want 200 (clamped to page 1)", p)
	}
}

func TestCallsPage_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/calls")
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "All Calls", "page heading missing")
}

func TestCallbacksPage_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/callbacks")
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "Callback Logs", "page heading missing")
}

// --- fragments ---

func TestFragmentRoutesReturn200(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []string{
		"/ui/fragments/stats",
		"/ui/fragments/recent-messages",
		"/ui/fragments/recent-calls",
		"/ui/fragments/messages-table",
		"/ui/fragments/calls-table",
		"/ui/fragments/callbacks-table",
	}
	for _, p := range cases {
		rec := get(t, h, p)
		assert.Equal(t, 200, rec.Code, "%s: want 200", p)
		// Fragments should render minimal content (no full HTML doc)
		assert.NotContains(t, rec.Body.String(), "<!DOCTYPE html>", "%s: fragment unexpectedly contains DOCTYPE (full page leak)", p)
	}
}

func TestStatsFragment_RendersCounts(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
		_ = s.SaveCall(ctx, &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := get(t, h, "/ui/fragments/stats")
	body := rec.Body.String()
	assert.Contains(t, body, ">2<", "stats fragment doesn't show 2 messages")
	assert.Contains(t, body, ">1<", "stats fragment doesn't show 1 call")
}

func TestMessageDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/fragments/message/SMmissing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Message not found", "not-found body missing")
}

func TestMessageDetail_Found(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{
			SID: "SM12345", Provider: "twilio",
			From: "+15550000001", To: "+15551234567",
			Body: "Hello world", Status: "delivered", CallbackURL: "http://app/cb",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	})
	rec := get(t, h, "/ui/fragments/message/SM12345")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	// html/template escapes "+" → "&#43;" in body output.
	for _, want := range []string{"SM12345", "Hello world", "&#43;15550000001", "&#43;15551234567", "http://app/cb"} {
		assert.Contains(t, body, want, "detail body missing %q", want)
	}
}

func TestCallDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/fragments/call/CAmissing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCallDetail_Found(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveCall(context.Background(), &storage.Call{
			SID: "CA12345", Provider: "twilio",
			From: "+1", To: "+2", Status: "completed", TwiMLURL: "http://example/twiml",
		})
	})
	rec := get(t, h, "/ui/fragments/call/CA12345")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "CA12345", "Call SID not in body")
	assert.Contains(t, body, "http://example/twiml", "TwiML URL not in body")
}

func TestCallbackDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []string{
		"/ui/fragments/callback-detail/99999",
		"/ui/fragments/callback-detail/notanumber",
	}
	for _, p := range cases {
		rec := get(t, h, p)
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s: want 404", p)
	}
}

func TestCallbackDetail_FoundWithEnrichedFields(t *testing.T) {
	var savedID int64
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		l := &storage.CallbackLog{
			TargetURL: "http://app/cb",
			Payload: `{"MessageSid":"SM1","MessageStatus":"delivered","From":"+1","To":"+2"}`,
			StatusCode: 200, AttemptNumber: 1,
		}
		_ = s.SaveCallbackLog(context.Background(), l)
		savedID = l.ID
	})
	rec := get(t, h, "/ui/fragments/callback-detail/"+stringFromInt(savedID))
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "delivered", "MessageStatus from payload not rendered")
	assert.Contains(t, body, "http://app/cb", "target URL not rendered")
	assert.Contains(t, body, "200", "status code 200 not rendered")
}

// --- enrichCallback unit ---

func TestEnrichCallback_ParsesPayload(t *testing.T) {
	log := &storage.CallbackLog{
		Payload: `{"MessageSid":"SM1","MessageStatus":"sent","To":"+1","From":"+2"}`,
	}
	row := enrichCallback(log)
	assert.Equal(t, "SM1", row.MessageSID)
	assert.Equal(t, "sent", row.MessageStatus)
	assert.Empty(t, row.CallSID, "call fields should be empty: %+v", row)
	assert.Empty(t, row.CallStatus, "call fields should be empty: %+v", row)
}

func TestEnrichCallback_EmptyPayload(t *testing.T) {
	log := &storage.CallbackLog{}
	row := enrichCallback(log)
	assert.Empty(t, row.MessageSID, "empty payload should yield empty fields: %+v", row)
	assert.Empty(t, row.CallSID, "empty payload should yield empty fields: %+v", row)
	assert.Empty(t, row.MessageStatus, "empty payload should yield empty fields: %+v", row)
	assert.Empty(t, row.CallStatus, "empty payload should yield empty fields: %+v", row)
}

func TestEnrichCallback_BadJSON(t *testing.T) {
	log := &storage.CallbackLog{Payload: "not json"}
	row := enrichCallback(log)
	assert.Empty(t, row.MessageSID, "bad JSON should yield empty fields: %+v", row)
}

// --- pagination math ---

func TestTotalPages(t *testing.T) {
	cases := []struct{ total, want int }{
		{0, 0},
		{1, 1},
		{50, 1},
		{51, 2},
		{100, 2},
		{101, 3},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, totalPages(tc.total), "totalPages(%d)", tc.total)
	}
}

// --- helpers ---

func stringFromInt(n int64) string { return strconv.FormatInt(n, 10) }
