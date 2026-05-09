// Package twilio implements provider.Provider with Twilio-compatible behavior.
package twilio

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"

	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/provider"
)

// Provider is the Twilio adapter. It is constructed from a config.Twilio.
type Provider struct {
	cfg *config.Twilio

	// Pre-built sets for O(1) membership checks; the lists in config are
	// expected to be small (handful to dozens), but maps make the intent clear
	// and keep IsKnownNumber from doing repeated O(N) scans on hot paths.
	successNums map[string]struct{}
	allowedFrom map[string]struct{}
	failureNums map[string]struct{}
}

// New returns a Twilio provider for the given config.
func New(cfg *config.Twilio) *Provider {
	return &Provider{
		cfg:         cfg,
		successNums: toSet(cfg.SuccessNumbers),
		allowedFrom: toSet(cfg.AllowedFromNumbers),
		failureNums: toSet(cfg.FailureNumbers),
	}
}

func toSet(xs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		out[x] = struct{}{}
	}
	return out
}

func (p *Provider) Name() string { return "twilio" }

// ValidateAuth parses HTTP Basic credentials from authHeader and compares
// them to config.account_sid / config.auth_token. accountSidFromURL is
// rejected if it disagrees with config.account_sid (Twilio behaves the same).
func (p *Provider) ValidateAuth(authHeader, accountSidFromURL string) error {
	if !p.cfg.Validation.RequireAuth {
		return nil
	}

	user, pass, ok := parseBasicAuth(authHeader)
	if !ok || user == "" || pass == "" {
		return authFailed()
	}
	if user != p.cfg.AccountSid || pass != p.cfg.AuthToken {
		return authFailed()
	}
	if accountSidFromURL != "" && accountSidFromURL != p.cfg.AccountSid {
		return authFailed()
	}
	return nil
}

// ValidateSMS runs (in order): parameter presence → phone format → From-allowlist.
// Each check is gated on its config toggle.
func (p *Provider) ValidateSMS(req provider.SMSRequest) error {
	if err := p.validateRequiredParams(req.From, req.To, req.Body, provider.RequiredSMSParams); err != nil {
		return err
	}
	if err := p.validatePhone(req.From, "From"); err != nil {
		return err
	}
	if err := p.validatePhone(req.To, "To"); err != nil {
		return err
	}
	if err := p.validateFromAllowlist(req.From); err != nil {
		return err
	}
	return nil
}

// ValidateCall mirrors ValidateSMS but with Url instead of Body.
func (p *Provider) ValidateCall(req provider.CallRequest) error {
	if err := p.validateRequiredParams(req.From, req.To, req.URL, provider.RequiredCallParams); err != nil {
		return err
	}
	if err := p.validatePhone(req.From, "From"); err != nil {
		return err
	}
	if err := p.validatePhone(req.To, "To"); err != nil {
		return err
	}
	if err := p.validateFromAllowlist(req.From); err != nil {
		return err
	}
	return nil
}

// validateRequiredParams reports the first missing param. Param names align with
// the Twilio API ("From", "To", "Body" / "From", "To", "Url"), so the error
// template's `{{ parameter }}` interpolation matches the user-facing field name.
func (p *Provider) validateRequiredParams(from, to, body string, names []string) error {
	if !p.cfg.Validation.RequireParameters {
		return nil
	}
	values := []string{from, to, body}
	for i, v := range values {
		if v == "" {
			return missingParameter(names[i])
		}
	}
	return nil
}

func (p *Provider) validatePhone(number, fieldName string) error {
	if !p.cfg.Validation.ValidatePhoneFormat {
		return nil
	}
	parsed, err := phonenumbers.Parse(number, "")
	if err != nil {
		return invalidPhoneNumber(fieldName, number)
	}
	if !phonenumbers.IsValidNumber(parsed) {
		return invalidPhoneNumber(fieldName, number)
	}
	return nil
}

func (p *Provider) validateFromAllowlist(from string) error {
	if !p.cfg.Validation.CheckFromNumbers {
		return nil
	}
	if _, ok := p.allowedFrom[from]; !ok {
		return invalidFromNumber(from)
	}
	return nil
}

// IsKnownNumber: To is in failure_numbers OR success_numbers. When both lists
// are empty, every number is unknown — the dispatcher will not progress the
// status flow and no callbacks fire.
func (p *Provider) IsKnownNumber(to string) bool {
	if _, ok := p.failureNums[to]; ok {
		return true
	}
	_, ok := p.successNums[to]
	return ok
}

// ShouldSucceed: failure_numbers → false; otherwise true (success_numbers).
// Only meaningful when IsKnownNumber returns true; the dispatcher short-circuits
// before calling this for unknown numbers.
func (p *Provider) ShouldSucceed(to string) bool {
	if _, ok := p.failureNums[to]; ok {
		return false
	}
	return true
}

// --- Basic auth parsing ---

// parseBasicAuth extracts (user, pass) from an `Authorization: Basic ...` header.
// Returns ok=false on any parse failure (missing scheme, bad base64, no colon).
func parseBasicAuth(authHeader string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", "", false
	}
	encoded := authHeader[len(prefix):]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	creds := string(decoded)
	colon := strings.IndexByte(creds, ':')
	if colon < 0 {
		return "", "", false
	}
	return creds[:colon], creds[colon+1:], true
}

// --- ValidationError constructors ---

func authFailed() *provider.ValidationError {
	return &provider.ValidationError{
		Code:         provider.ErrCodeAuthFailed,
		Message:      "Authenticate",
		HTTPStatus:   401,
		TemplateName: provider.TemplateAuthFailed,
	}
}

func missingParameter(name string) *provider.ValidationError {
	return &provider.ValidationError{
		Code:         provider.ErrCodeMissingParameter,
		Message:      fmt.Sprintf("The required parameter '%s' is missing.", name),
		HTTPStatus:   400,
		TemplateName: provider.TemplateMissingParameter,
		Vars:         map[string]string{"parameter": name},
	}
}

func invalidPhoneNumber(field, number string) *provider.ValidationError {
	return &provider.ValidationError{
		Code:         provider.ErrCodeInvalidPhoneNumber,
		Message:      fmt.Sprintf("The '%s' number %s is not a valid phone number.", field, number),
		HTTPStatus:   400,
		TemplateName: provider.TemplateInvalidPhoneNumber,
		Vars:         map[string]string{"field": field, "number": number},
	}
}

func invalidFromNumber(number string) *provider.ValidationError {
	return &provider.ValidationError{
		Code:         provider.ErrCodeInvalidFromNumber,
		Message:      fmt.Sprintf("The 'From' phone number %s is not a valid, message-capable Twilio phone number.", number),
		HTTPStatus:   400,
		TemplateName: provider.TemplateInvalidFromNumber,
		Vars:         map[string]string{"from_number": number},
	}
}

// Compile-time interface satisfaction check.
var _ provider.Provider = (*Provider)(nil)
