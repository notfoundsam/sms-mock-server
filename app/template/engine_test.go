package template

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime returns a deterministic Now() for tests.
func fixedTime() time.Time {
	return time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
}

// realTemplatesEngine builds an Engine from the actual templates/ directory
// committed to the repo. Tests use this for end-to-end golden-file checks.
func realTemplatesEngine(t *testing.T, opts ...func(*Options)) *Engine {
	t.Helper()
	o := Options{
		Provider: "twilio",
		Now:      fixedTime,
	}
	for _, fn := range opts {
		fn(&o)
	}
	e, err := New(os.DirFS("../templates"), o)
	require.NoError(t, err, "New")
	return e
}

func TestRenderSMSResponse_SuccessIsValidJSON(t *testing.T) {
	e := realTemplatesEngine(t)

	out, err := e.RenderSMSResponse(SMSResponseData{
		MessageSID: "SM123",
		AccountSid: "ACtest",
		Status:     "queued",
		Request:    SMSRequestView{From: "+12025551234", To: "+12025550100", Body: "hello"},
	}, true)
	require.NoError(t, err, "RenderSMSResponse")

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "output is not valid JSON: %s", out)

	checks := map[string]any{
		"sid":         "SM123",
		"account_sid": "ACtest",
		"status":      "queued",
		"to":          "+12025550100",
		"from":        "+12025551234",
		"body":        "hello",
		"direction":   "outbound-api",
		"api_version": "2010-04-01",
	}
	for k, want := range checks {
		assert.Equal(t, want, got[k], "key %s", k)
	}

	wantURI := "/2010-04-01/Accounts/ACtest/Messages/SM123.json"
	assert.Equal(t, wantURI, got["uri"])

	// num_segments is "1" string per Twilio
	assert.Equal(t, "1", got["num_segments"])
}

func TestRenderSMSResponse_FailureUsesFailureTemplate(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderSMSResponse(SMSResponseData{
		MessageSID: "SM123", AccountSid: "ACtest", Status: "failed",
		Request: SMSRequestView{From: "+12025551234", To: "+12025550199", Body: "x"},
	}, false)
	require.NoError(t, err, "RenderSMSResponse")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON")
	assert.Equal(t, "failed", got["status"])
}

// JSON-escaping is the central correctness concern for text/template output:
// Body containing quotes / backslashes / newlines must produce valid JSON.
func TestRenderSMSResponse_JSONEscapingForBody(t *testing.T) {
	e := realTemplatesEngine(t)

	tricky := "She said \"hi\".\nBackslash: \\ and tab:\there"
	out, err := e.RenderSMSResponse(SMSResponseData{
		MessageSID: "SM1", AccountSid: "AC1", Status: "queued",
		Request: SMSRequestView{From: "+12025551234", To: "+12025550100", Body: tricky},
	}, true)
	require.NoError(t, err, "Render")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed), "escaping broken: output: %s", out)
	assert.Equal(t, tricky, parsed["body"], "body roundtrip mismatch")
}

func TestRenderSMSResponse_TimestampsFilledByEngine(t *testing.T) {
	e := realTemplatesEngine(t)
	out, _ := e.RenderSMSResponse(SMSResponseData{
		MessageSID: "SM1", AccountSid: "AC1", Status: "queued",
		Request: SMSRequestView{From: "+12025551234", To: "+12025550100", Body: "x"},
	}, true)
	var got map[string]any
	_ = json.Unmarshal(out, &got)

	wantTS := "Thu, 15 Jan 2026 10:30:00 +0000"
	assert.Equal(t, wantTS, got["date_created"])
	assert.Equal(t, wantTS, got["date_updated"])
	assert.Nil(t, got["date_sent"])
}

func TestRenderCallResponse_Success(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderCallResponse(CallResponseData{
		CallSID: "CA1", AccountSid: "AC1", Status: "queued",
		Request: CallRequestView{From: "+12025551234", To: "+12025550100", URL: "http://example/twiml"},
	}, true)
	require.NoError(t, err, "RenderCallResponse")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON: %s", out)
	assert.Equal(t, "CA1", got["sid"])
	assert.Equal(t, "/2010-04-01/Accounts/AC1/Calls/CA1.json", got["uri"])
	assert.Equal(t, "+12025550100", got["to_formatted"])
	assert.Equal(t, "+12025551234", got["from_formatted"])
}

// --- error templates ---

func TestRenderError_AuthFailed(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderError("auth_failed.json", nil)
	require.NoError(t, err, "RenderError")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON")
	assert.EqualValues(t, 20003, got["code"])
	assert.EqualValues(t, 401, got["status"])
}

func TestRenderError_MissingParameter(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderError("missing_parameter.json", map[string]string{"parameter": "From"})
	require.NoError(t, err, "RenderError")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON")
	assert.EqualValues(t, 21604, got["code"])
	assert.Equal(t, "The required parameter 'From' is missing.", got["message"])
}

func TestRenderError_InvalidPhoneNumber(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderError("invalid_phone_number.json", map[string]string{"field": "To", "number": "+1234"})
	require.NoError(t, err, "RenderError")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON")
	assert.EqualValues(t, 21211, got["code"])
	want := "The 'To' number +1234 is not a valid phone number."
	assert.Equal(t, want, got["message"])
}

func TestRenderError_InvalidFromNumber(t *testing.T) {
	e := realTemplatesEngine(t)
	out, err := e.RenderError("invalid_from_number.json", map[string]string{"from_number": "+19998887777"})
	require.NoError(t, err, "RenderError")
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got), "invalid JSON")
	assert.EqualValues(t, 21606, got["code"])
	assert.Contains(t, got["message"].(string), "+19998887777", "message doesn't include the rejected number")
}

func TestRenderError_UnknownTemplate(t *testing.T) {
	e := realTemplatesEngine(t)
	_, err := e.RenderError("nope.json", nil)
	require.Error(t, err, "expected error for unknown template")
}

// Adversarial input in error-template variables must not break the JSON
// envelope. Without `| jsonStr` escaping in the templates, a quote or
// backslash in `field` / `number` / `from_number` / `parameter` produces
// unparseable JSON.
func TestRenderError_JSONEscapingForVars(t *testing.T) {
	e := realTemplatesEngine(t)

	cases := []struct {
		template string
		vars     map[string]string
	}{
		{
			template: "missing_parameter.json",
			vars:     map[string]string{"parameter": `bad"name'with\backslash`},
		},
		{
			template: "invalid_phone_number.json",
			vars:     map[string]string{"field": `bad"field`, "number": "+\"injected\""},
		},
		{
			template: "invalid_from_number.json",
			vars:     map[string]string{"from_number": `+"injected\and"escape`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			out, err := e.RenderError(tc.template, tc.vars)
			require.NoError(t, err)
			// Must round-trip through json.Unmarshal — that's the property
			// adversarial input was breaking before the fix.
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(out, &parsed),
				"output is not valid JSON:\n%s", out)
		})
	}
}

// jsonStr funcmap: same escaping as `json` but without the surrounding quotes.
// Used inside existing JSON string literals in error templates.
func TestJSONStrFunc(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", `hello`},
		{`with "quotes"`, `with \"quotes\"`},
		{"line1\nline2", `line1\nline2`},
		{`back\slash`, `back\\slash`},
	}
	for _, tc := range cases {
		got, err := jsonStr(tc.in)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "jsonStr(%q)", tc.in)
	}
}

// --- funcmap helpers ---

func TestJSONFunc(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", `"hello"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{"line1\nline2", `"line1\nline2"`},
		{`back\slash`, `"back\\slash"`},
		{42, `42`},
		{nil, `null`},
	}
	for _, tc := range cases {
		got, err := jsonValue(tc.in)
		// assert (not require) so a single failure doesn't abort the whole table
		if !assert.NoError(t, err, "jsonValue(%v)", tc.in) { //nolint:testifylint // see comment
			continue
		}
		assert.Equal(t, tc.want, got, "jsonValue(%v)", tc.in)
	}
}

func TestAssetURL(t *testing.T) {
	manifest := map[string]string{
		"/static/css/style.css": "/static/dist/style.abc123.css",
	}
	f := assetURL(manifest)
	assert.Equal(t, "/static/dist/style.abc123.css", f("/static/css/style.css"), "known asset")
	assert.Equal(t, "/static/css/missing.css", f("/static/css/missing.css"), "unknown asset should return original path")
}

func TestFormatTimeIn(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	f := formatTimeIn(tokyo)
	utc := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-01-15 09:00:00", f(utc), "formatTimeIn(Tokyo)")
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"x", 0, "x"}, // n<=0 returns input unchanged
		{"abc", 1, "…"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, truncate(tc.s, tc.n), "truncate(%q, %d)", tc.s, tc.n)
	}
}

// --- engine construction edge cases ---

func TestNew_RequiresProvider(t *testing.T) {
	_, err := New(os.DirFS("../templates"), Options{})
	assert.Error(t, err, "expected error for missing Provider")
}

func TestNew_FailsOnMissingResponseDir(t *testing.T) {
	// fstest with only an errors dir — no responses
	fsys := fstest.MapFS{
		"errors/twilio/auth_failed.json": &fstest.MapFile{Data: []byte(`{}`)},
	}
	_, err := New(fsys, Options{Provider: "twilio"})
	assert.Error(t, err, "expected error for missing responses dir")
}

func TestRenderUI_UnknownTemplateReturnsError(t *testing.T) {
	e := realTemplatesEngine(t)
	err := e.RenderUI(&bytes.Buffer{}, "no_such_template.html", nil)
	assert.Error(t, err, "expected RenderUI error for unknown template name")
}
