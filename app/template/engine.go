// Package template renders the JSON response/error templates and HTML UI
// pages from an embedded filesystem.
//
// The engine wraps two stdlib template packages:
//   - text/template for JSON output (no auto-escaping; we control escaping
//     via the `json` funcmap helper)
//   - html/template for HTML UI pages (auto-escaping)
//
// Templates are loaded from an fs.FS at construction. Production passes an
// embed.FS rooted at "templates/"; tests can use os.DirFS for golden checks.
package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"path"
	"strings"
	texttemplate "text/template"
	"time"
)

// Engine holds parsed templates.
type Engine struct {
	provider string

	responses *texttemplate.Template // text/template, JSON output, namespaced by relative path
	errors    *texttemplate.Template

	// UI templates use Jinja-style inheritance via Go's block/define mechanism.
	// Because each page redefines `title` and `content`, we can't keep all pages
	// in one template set (later parses would overwrite earlier ones — verified
	// experimentally). Instead: uiBase is a shared set holding base.html + all
	// fragment templates; uiPages maps each page filename to a Clone() of uiBase
	// with that page's defines parsed in.
	uiBase  *htmltemplate.Template            // base.html + fragments — shared across pages
	uiPages map[string]*htmltemplate.Template // "dashboard.html" → cloned set with that page's defines

	now func() time.Time // injectable for deterministic timestamps in tests
}

// FuncMap is an alias users of this package can pass without importing
// text/template directly.
type FuncMap = map[string]any

// Options configures engine construction.
type Options struct {
	// Provider is the carrier name ("twilio"). Used to select the
	// templates/responses/{provider} and templates/errors/{provider} subtrees.
	Provider string

	// AssetManifest maps original paths to versioned paths. Templates call
	// {{ assetUrl "/static/css/style.css" }} which returns the manifest entry,
	// or the original path if absent (matching app/ui.py behavior).
	AssetManifest map[string]string

	// Timezone is the location used by the formatTime template func. Defaults
	// to UTC if nil.
	Timezone *time.Location

	// Now is an optional override of time.Now.UTC for deterministic tests.
	// If nil, time.Now().UTC() is used.
	Now func() time.Time
}

// New parses templates from fsys (rooted such that fsys contains "responses",
// "errors", and optionally "ui" subdirs).
func New(fsys fs.FS, opts Options) (*Engine, error) {
	if opts.Provider == "" {
		return nil, errors.New("template: Options.Provider is required")
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	tz := opts.Timezone
	if tz == nil {
		tz = time.UTC
	}

	e := &Engine{provider: opts.Provider, now: now}

	jsonFuncs := texttemplate.FuncMap{
		"json":    jsonValue,
		"jsonStr": jsonStr,
	}

	uiFuncs := htmltemplate.FuncMap{
		"assetUrl":   assetURL(opts.AssetManifest),
		"formatTime": formatTimeIn(tz),
		"truncate":   truncate,
		"upper":      strings.ToUpper,
		"hasPrefix":  strings.HasPrefix,
		"add":        func(a, b int) int { return a + b },
		"sub":        func(a, b int) int { return a - b },
	}

	respDir := path.Join("responses", opts.Provider)
	errDir := path.Join("errors", opts.Provider)

	resp, err := parseTextDir(fsys, respDir, jsonFuncs)
	if err != nil {
		return nil, fmt.Errorf("parse response templates: %w", err)
	}
	e.responses = resp

	errs, err := parseTextDir(fsys, errDir, jsonFuncs)
	if err != nil {
		return nil, fmt.Errorf("parse error templates: %w", err)
	}
	e.errors = errs

	// UI templates: optional. With no ui/ subdir, leaves uiBase/uiPages nil and
	// RenderUI returns "no ui templates loaded".
	uiBase, uiPages, err := parseUIDir(fsys, "ui", uiFuncs)
	if err != nil {
		return nil, fmt.Errorf("parse ui templates: %w", err)
	}
	e.uiBase = uiBase
	e.uiPages = uiPages

	return e, nil
}

// --- rendering ---

// SMSResponseData carries all fields needed to render send_sms_*.json.
// `Now` and `MessageSID` are populated by the caller; `AccountSid` and
// `Status` come from config and the dispatcher decision respectively.
type SMSResponseData struct {
	MessageSID  string
	AccountSid  string
	Status      string
	NumSegments int
	DateCreated string
	DateUpdated string
	Request     SMSRequestView
}

// SMSRequestView is the subset of SMSRequest that templates expose. It
// mirrors the Jinja `request.To`/`request.From`/`request.Body` access pattern
// from the Python templates.
type SMSRequestView struct {
	From string
	To   string
	Body string
}

// CallResponseData carries fields for make_call_*.json.
type CallResponseData struct {
	CallSID     string
	AccountSid  string
	Status      string
	DateCreated string
	DateUpdated string
	Request     CallRequestView
}

type CallRequestView struct {
	From string
	To   string
	URL  string
}

// RenderSMSResponse renders send_sms_success.json (success=true) or
// send_sms_failure.json (success=false), populating standard timestamp fields
// when the data carrier didn't supply them.
func (e *Engine) RenderSMSResponse(data SMSResponseData, success bool) ([]byte, error) {
	name := "send_sms_success.json"
	if !success {
		name = "send_sms_failure.json"
	}
	if data.NumSegments == 0 {
		data.NumSegments = 1
	}
	e.fillTimestamps(&data.DateCreated, &data.DateUpdated)
	return e.renderResponse(name, data)
}

// RenderCallResponse renders make_call_*.json by the same pattern.
func (e *Engine) RenderCallResponse(data CallResponseData, success bool) ([]byte, error) {
	name := "make_call_success.json"
	if !success {
		name = "make_call_failure.json"
	}
	e.fillTimestamps(&data.DateCreated, &data.DateUpdated)
	return e.renderResponse(name, data)
}

// RenderError renders templates/errors/{provider}/{name} with vars. vars is
// passed directly as the template's `.` — Go templates support map field
// access, so an error template can reference `{{ .parameter }}`, `{{ .field }}`, etc.
func (e *Engine) RenderError(name string, vars map[string]string) ([]byte, error) {
	if e.errors == nil {
		return nil, errors.New("no error templates loaded")
	}
	t := e.errors.Lookup(name)
	if t == nil {
		return nil, fmt.Errorf("error template %q not found", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return nil, fmt.Errorf("execute error template %q: %w", name, err)
	}
	return buf.Bytes(), nil
}

// RenderUI executes the named HTML template into w.
//
// Page templates (e.g. "dashboard.html") are full pages: each one is a cloned
// set holding base.html + that page's defines. RenderUI executes the cloned
// set's "base" entry — the page's defines override the placeholders in base.
//
// Fragment templates (e.g. "fragments/stats.html") render via uiBase directly
// (they don't extend base, they're self-contained chunks for HTMX swaps).
func (e *Engine) RenderUI(w io.Writer, name string, data any) error {
	if e.uiBase == nil {
		return errors.New("no ui templates loaded")
	}
	if pageSet, ok := e.uiPages[name]; ok {
		// Page: render via "base" entrypoint inside that page's cloned set.
		if err := pageSet.ExecuteTemplate(w, "base", data); err != nil {
			return fmt.Errorf("execute page template %q: %w", name, err)
		}
		return nil
	}
	// Fragment: render directly out of the shared base.
	if t := e.uiBase.Lookup(name); t != nil {
		if err := t.Execute(w, data); err != nil {
			return fmt.Errorf("execute fragment %q: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("ui template %q not found", name)
}

func (e *Engine) renderResponse(name string, data any) ([]byte, error) {
	t := e.responses.Lookup(name)
	if t == nil {
		return nil, fmt.Errorf("response template %q not found", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute response template %q: %w", name, err)
	}
	return buf.Bytes(), nil
}

func (e *Engine) fillTimestamps(created, updated *string) {
	if *created == "" {
		*created = formatRFC2822(e.now())
	}
	if *updated == "" {
		*updated = *created
	}
}

// formatRFC2822 produces Twilio's standard date format
// (RFC 2822 / e.g. "Wed, 15 Jan 2024 10:30:00 +0000").
func formatRFC2822(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700")
}

// --- funcmap helpers ---

// jsonValue marshals v as a JSON literal — used in templates for safe
// interpolation of arbitrary string fields:
//
//	"body": {{ .Request.Body | json }}
//
// The result includes surrounding quotes for strings, so the template should
// NOT wrap it in quotes itself.
func jsonValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(b), nil
}

// jsonStr escapes v's string contents per JSON-string rules but does NOT
// emit the surrounding quotes. Use it inside an existing JSON string literal
// to inject user-controlled content safely:
//
//	"message": "The '{{ .field | jsonStr }}' number {{ .number | jsonStr }} is invalid."
//
// Without this, a value containing `"` or `\` would break the JSON envelope.
func jsonStr(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	// json.Marshal of a string yields a quoted string; strip the outer quotes.
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		return string(b[1 : len(b)-1]), nil
	}
	return string(b), nil
}

func assetURL(manifest map[string]string) func(string) string {
	return func(p string) string {
		if v, ok := manifest[p]; ok {
			return v
		}
		return p
	}
}

func formatTimeIn(loc *time.Location) func(time.Time) string {
	return func(t time.Time) string {
		return t.In(loc).Format("2006-01-02 15:04:05")
	}
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}

// --- template loading ---

// parseTextDir loads every *.json (or .* file) under dir as a text/template,
// keyed by filename (no path prefix), with funcs available.
func parseTextDir(fsys fs.FS, dir string, funcs texttemplate.FuncMap) (*texttemplate.Template, error) {
	root := texttemplate.New("").Funcs(funcs)
	count, err := walkAndParse(fsys, dir, func(name string, body []byte) error {
		if _, err := root.New(name).Parse(string(body)); err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("no templates found under %s", dir)
	}
	return root, nil
}

// parseUIDir parses HTML templates under dir into a base set + per-page clones.
//
// Files at the top level of dir are classified as either:
//   - "base.html" (the layout) → added to the base set
//   - any other "*.html" page → its own cloned set (base + page defines)
//
// Files under dir/fragments/ are added to the base set as fragment templates,
// addressable by their relative path ("fragments/stats.html" etc.).
//
// Returns (uiBase, uiPages). uiBase is non-nil only if at least one .html
// file was found; otherwise both are nil and RenderUI will report "no ui
// templates loaded".
func parseUIDir(fsys fs.FS, dir string, funcs htmltemplate.FuncMap) (*htmltemplate.Template, map[string]*htmltemplate.Template, error) {
	if _, err := fs.Stat(fsys, dir); err != nil {
		return nil, nil, nil //nolint:nilerr // missing ui/ dir is intentionally not an error
	}

	type fileEntry struct {
		rel  string // path relative to dir, e.g. "dashboard.html" or "fragments/stats.html"
		body []byte
	}
	var files []fileEntry
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		files = append(files, fileEntry{
			rel:  strings.TrimPrefix(p, dir+"/"),
			body: body,
		})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, nil, nil
	}

	// Build base set: base.html + all fragments.
	uiBase := htmltemplate.New("").Funcs(funcs)
	var pageFiles []fileEntry
	for _, f := range files {
		switch {
		case f.rel == "base.html":
			if _, err := uiBase.New("base").Parse(string(f.body)); err != nil {
				return nil, nil, fmt.Errorf("parse base.html: %w", err)
			}
		case strings.HasPrefix(f.rel, "fragments/"):
			if _, err := uiBase.New(f.rel).Parse(string(f.body)); err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", f.rel, err)
			}
		default:
			// Top-level non-base page → defer until uiBase is fully parsed,
			// so each clone gets all fragments.
			pageFiles = append(pageFiles, f)
		}
	}

	// One cloned set per page so each page's `define` blocks are isolated.
	uiPages := make(map[string]*htmltemplate.Template, len(pageFiles))
	for _, p := range pageFiles {
		clone, err := uiBase.Clone()
		if err != nil {
			return nil, nil, fmt.Errorf("clone uiBase for %s: %w", p.rel, err)
		}
		if _, err := clone.New(p.rel).Parse(string(p.body)); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", p.rel, err)
		}
		uiPages[p.rel] = clone
	}
	return uiBase, uiPages, nil
}

// walkAndParse walks dir in fsys and invokes parseFile for each regular file.
// Returns the number of files parsed. Missing dir is not an error (returns 0).
func walkAndParse(fsys fs.FS, dir string, parseFile func(name string, body []byte) error) (int, error) {
	if _, err := fs.Stat(fsys, dir); err != nil {
		// missing dir is not an error
		return 0, nil //nolint:nilerr // intentional: caller checks the count
	}
	count := 0
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		// name relative to dir, e.g. "responses/twilio/x.json" → "x.json";
		// "ui/fragments/stats.html" → "fragments/stats.html"
		rel := strings.TrimPrefix(p, dir+"/")
		if err := parseFile(rel, body); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return count, fmt.Errorf("walk %s: %w", dir, err)
	}
	return count, nil
}
