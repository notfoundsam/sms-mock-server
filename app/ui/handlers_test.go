package ui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
// and a FakeStore. Use mutate to seed the store before tests. The
// hideDeleteAllButton flag is false by default; tests that want to verify
// the button hides should call newTestHandlerWithHideButton.
func newTestHandler(t *testing.T, mutate ...func(*testutil.FakeStore)) (http.Handler, *testutil.FakeStore) {
	return newTestHandlerWithHideButton(t, false, mutate...)
}

func newTestHandlerWithHideButton(t *testing.T, hide bool, mutate ...func(*testutil.FakeStore)) (http.Handler, *testutil.FakeStore) {
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
	h := New(logger, store, engine, "twilio", "UTC", hide)

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

func req(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	h.ServeHTTP(rec, r)
	return rec
}

// --- pages ---

func TestRoot_RendersMessagesPage(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/")
	require.Equal(t, 200, rec.Code, "body=%s", rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "<title>Messages · SMS Mock</title>")
	assert.Contains(t, body, "Messages") // sidebar nav
	assert.Contains(t, body, "Calls")    // sidebar nav
	assert.Contains(t, body, "TWILIO")
	assert.Contains(t, body, "No messages")
}

func TestRoot_WithMessages(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{
			SID: "SM1", Provider: "twilio", From: "+15550000001", To: "+15551234567",
			Body: "hello world", Status: "delivered",
		})
	})
	rec := get(t, h, "/")
	body := rec.Body.String()
	assert.Contains(t, body, "&#43;15550000001", "phone numbers HTML-escaped")
	assert.Contains(t, body, "hello world")
	assert.Contains(t, body, "delivered")
	assert.Contains(t, body, "/view/messages/SM1", "row should link to detail")
}

func TestRoot_QFilter(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Body: "alpha", Status: "delivered", Provider: "twilio"})
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", From: "+1", To: "+2", Body: "beta", Status: "delivered", Provider: "twilio"})
	})
	rec := get(t, h, "/?q=alpha")
	body := rec.Body.String()
	assert.Contains(t, body, "alpha")
	assert.NotContains(t, body, "beta")
}

func TestRoot_StatusFilter(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", From: "+1", To: "+2", Body: "alpha", Status: "delivered", Provider: "twilio"})
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM2", From: "+1", To: "+2", Body: "beta", Status: "queued", Provider: "twilio"})
	})
	rec := get(t, h, "/?status=delivered")
	body := rec.Body.String()
	assert.Contains(t, body, "alpha")
	assert.NotContains(t, body, "beta", "queued message should be filtered out")
}

func TestCalls_Page(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveCall(context.Background(), &storage.Call{
			SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "completed",
		})
	})
	rec := get(t, h, "/calls")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<title>Calls · SMS Mock</title>")
	assert.Contains(t, body, "/view/calls/CA1")
	assert.Contains(t, body, "completed")
}

func TestRetiredRoutes_Return404(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/ui/messages", "/ui/calls", "/ui/callbacks", "/ui/fragments/stats", "/ui/fragments/recent-messages"} {
		rec := get(t, h, p)
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s should be 404", p)
	}
}

// --- detail ---

func TestMessageDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/view/messages/SMmissing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Message not found")
}

func TestMessageDetail_FoundMarksRead(t *testing.T) {
	var store *testutil.FakeStore
	h, store := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{
			SID: "SM12345", Provider: "twilio",
			From: "+15550000001", To: "+15551234567",
			Body: "Hello world", Status: "delivered", CallbackURL: "http://app/cb",
		})
	})
	rec := get(t, h, "/view/messages/SM12345")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	for _, want := range []string{"SM12345", "Hello world", "&#43;15550000001", "http://app/cb"} {
		assert.Contains(t, body, want, "detail body missing %q", want)
	}

	// Side effect: opening detail marks read.
	got, err := store.GetMessage(context.Background(), "SM12345")
	require.NoError(t, err)
	assert.True(t, got.IsRead, "MarkMessageRead should have run")
}

func TestMessageDetail_NotFound_DoesNotMarkRead(t *testing.T) {
	h, store := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := get(t, h, "/view/messages/SMmissing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	got, _ := store.GetMessage(context.Background(), "SM1")
	assert.False(t, got.IsRead, "no other message should have been marked read")
}

func TestMessageDetail_CallbackSummary(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "delivered"})
		_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: `{"MessageSid":"SM1","MessageStatus":"sent"}`, StatusCode: 200, ResponseBody: "ok"})
		_ = s.SaveCallbackLog(ctx, &storage.CallbackLog{TargetURL: "x", Payload: `{"MessageSid":"SM1","MessageStatus":"delivered"}`, StatusCode: 200, ResponseBody: "ok"})
	})
	rec := get(t, h, "/view/messages/SM1")
	body := rec.Body.String()
	assert.Contains(t, body, "Callback delivery")
	assert.Contains(t, body, "HTTP 200")
	assert.Contains(t, body, "2 attempts")
}

func TestMessageDetail_NoCallbackSummaryWhenAbsent(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "delivered"})
	})
	rec := get(t, h, "/view/messages/SM1")
	assert.NotContains(t, rec.Body.String(), "Callback delivery")
}

func TestCallDetail(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveCall(context.Background(), &storage.Call{
			SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "completed", TwiMLURL: "http://example/twiml",
		})
	})
	rec := get(t, h, "/view/calls/CA1")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "CA1")
	assert.Contains(t, body, "http://example/twiml")
}

// --- fragments ---

func TestListFragment_Messages(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Body: "hi", Status: "delivered"})
	})
	rec := get(t, h, "/ui/fragments/list?type=messages")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "SM1")
	assert.NotContains(t, body, "<!DOCTYPE", "fragments don't render full pages")
}

func TestListFragment_Calls(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveCall(context.Background(), &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "completed"})
	})
	rec := get(t, h, "/ui/fragments/list?type=calls")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "/view/calls/CA1")
}

func TestSidebarFragment_NoTagsHidesSection(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := get(t, h, "/ui/fragments/sidebar?type=messages")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "<h3 class=\"sidebar-heading\">Tags</h3>",
		"Tags section should not render when no records are tagged")
}

func TestSidebarFragment_ShowsTagWhenAttached(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		ctx := context.Background()
		_ = s.SaveMessage(ctx, &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
		m, _ := s.GetMessage(ctx, "SM1")
		_ = s.SetMessageTags(ctx, m.ID, []string{"verification", "auth"})
	})
	rec := get(t, h, "/ui/fragments/sidebar?type=messages")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<h3 class=\"sidebar-heading\">Tags</h3>")
	assert.Contains(t, body, ">verification<")
	assert.Contains(t, body, ">auth<")
}

func TestSidebarFragment_ActiveType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := get(t, h, "/ui/fragments/sidebar?type=calls")
	body := rec.Body.String()
	// The Calls nav item should carry active class.
	idx := strings.Index(body, `href="/calls"`)
	require.NotEqual(t, -1, idx, "calls nav not rendered")
	// Find class attribute around it
	end := idx + 200
	if end > len(body) {
		end = len(body)
	}
	assert.Contains(t, body[idx:end], "active", "calls link should be active when type=calls")
}

// --- delete ---

func TestDeleteMessage_Success(t *testing.T) {
	h, store := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := req(t, h, "DELETE", "/ui/messages/SM1")
	require.Equal(t, http.StatusNoContent, rec.Code)
	_, err := store.GetMessage(context.Background(), "SM1")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestDeleteMessage_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := req(t, h, "DELETE", "/ui/messages/SMmissing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteMessage_WrongMethod(t *testing.T) {
	h, _ := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveMessage(context.Background(), &storage.Message{SID: "SM1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := req(t, h, "POST", "/ui/messages/SM1")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "Go 1.22 mux returns 405 for method mismatch")
}

func TestDeleteCall_Success(t *testing.T) {
	h, store := newTestHandler(t, func(s *testutil.FakeStore) {
		_ = s.SaveCall(context.Background(), &storage.Call{SID: "CA1", Provider: "twilio", From: "+1", To: "+2", Status: "queued"})
	})
	rec := req(t, h, "DELETE", "/ui/calls/CA1")
	require.Equal(t, http.StatusNoContent, rec.Code)
	_, err := store.GetCall(context.Background(), "CA1")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

// --- pagination ---

func TestPagination_Math(t *testing.T) {
	cases := []struct{ total, want int }{{0, 0}, {1, 1}, {50, 1}, {51, 2}, {100, 2}, {101, 3}}
	for _, tc := range cases {
		assert.Equal(t, tc.want, totalPages(tc.total), "totalPages(%d)", tc.total)
	}
}

func TestRoot_PaginationInvalidPageClampsToOne(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/?page=0", "/?page=-5", "/?page=garbage"} {
		rec := get(t, h, p)
		assert.Equal(t, 200, rec.Code, "%s should clamp to page 1", p)
	}
}

// --- helpers ---

func TestNormalizeType(t *testing.T) {
	assert.Equal(t, "messages", normalizeType(""))
	assert.Equal(t, "messages", normalizeType("garbage"))
	assert.Equal(t, "messages", normalizeType("messages"))
	assert.Equal(t, "calls", normalizeType("calls"))
}


func TestSidebar_DeleteAllButtonShownWhenFlagOff(t *testing.T) {
	h, _ := newTestHandlerWithHideButton(t, false)
	rec := get(t, h, "/")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `action="/clear/messages"`,
		"delete-all form should be present when HideDeleteAllButton=false")
	assert.Contains(t, body, "Delete all messages")
}

func TestSidebar_DeleteAllButtonHiddenWhenFlagOn(t *testing.T) {
	h, _ := newTestHandlerWithHideButton(t, true)
	rec := get(t, h, "/")
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, `action="/clear/messages"`,
		"delete-all form should be absent when HideDeleteAllButton=true")
	assert.NotContains(t, body, "Delete all messages")
}

func TestSidebarFragment_DeleteAllButtonHonorsFlag(t *testing.T) {
	// The fragment endpoint is what HTMX polls every 3s; verify the flag
	// applies to the fragment too, not just the initial page render.
	hOff, _ := newTestHandlerWithHideButton(t, false)
	rec := get(t, hOff, "/ui/fragments/sidebar?type=messages")
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "Delete all messages")

	hOn, _ := newTestHandlerWithHideButton(t, true)
	rec = get(t, hOn, "/ui/fragments/sidebar?type=messages")
	require.Equal(t, 200, rec.Code)
	assert.NotContains(t, rec.Body.String(), "Delete all messages")
}
