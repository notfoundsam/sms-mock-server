package httpapi

import (
	"net/http"

	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
)

// SendMessage handles POST /2010-04-01/Accounts/{AccountSid}/Messages.json.
//
// Flow (mirrors `app/main.py:251-325`):
//  1. Parse form
//  2. ValidateAuth → ValidateSMS (provider enforces toggle gating)
//  3. Determine outcome via IsKnownNumber + ShouldSucceed
//  4. Generate SID, persist message at status="queued"
//  5. Render initial response template (always the success template — initial
//     status is queued either way), write 201
//  6. Schedule callback flow — non-blocking, NOT tied to r.Context()
func (s *Server) SendMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.logger.Warn("ParseForm failed", "error", err)
		writeJSONString(w, http.StatusBadRequest, `{"code":400,"message":"malformed request","status":400}`)
		return
	}

	accountSid := r.PathValue("AccountSid")
	req := provider.SMSRequest{
		AccountSid:     accountSid,
		From:           r.PostFormValue("From"),
		To:             r.PostFormValue("To"),
		Body:           r.PostFormValue("Body"),
		StatusCallback: r.PostFormValue("StatusCallback"),
	}

	if err := s.provider.ValidateAuth(r.Header.Get("Authorization"), accountSid); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.provider.ValidateSMS(req); err != nil {
		s.writeError(w, err)
		return
	}

	isKnown := s.provider.IsKnownNumber(req.To)
	willSucceed := false
	if isKnown {
		willSucceed = s.provider.ShouldSucceed(req.To)
	}

	sid := generateSID("SM")
	msg := &storage.Message{
		SID: sid, Provider: s.provider.Name(),
		From: req.From, To: req.To, Body: req.Body,
		Status: "queued", CallbackURL: req.StatusCallback,
	}
	if err := s.store.SaveMessage(r.Context(), msg); err != nil {
		s.logger.Error("SaveMessage failed", "sid", sid, "error", err)
		writeJSONString(w, http.StatusInternalServerError,
			`{"code":500,"message":"storage error","status":500}`)
		return
	}

	body, err := s.tmpl.RenderSMSResponse(tmpl.SMSResponseData{
		MessageSID: sid,
		AccountSid: s.accountSid,
		Status:     "queued",
		Request: tmpl.SMSRequestView{
			From: req.From, To: req.To, Body: req.Body,
		},
	}, true)
	if err != nil {
		s.logger.Error("render SMS response failed", "sid", sid, "error", err)
		writeJSONString(w, http.StatusInternalServerError,
			`{"code":500,"message":"render error","status":500}`)
		return
	}

	s.logger.Info("SMS created",
		"sid", sid, "from", req.From, "to", req.To,
		"is_known", isKnown, "will_succeed", willSucceed)

	writeJSONBytes(w, http.StatusCreated, body)

	// Schedule status flow AFTER response is written, with the
	// effective callback URL (empty if globally disabled).
	cbURL := req.StatusCallback
	if !s.callbacksEnabled {
		cbURL = ""
	}
	s.dispatcher.ScheduleSMSStatusFlow(sid, req.From, req.To, cbURL, isKnown, willSucceed)
}

// MakeCall handles POST /2010-04-01/Accounts/{AccountSid}/Calls.json.
// Mirrors SendMessage with the call-specific differences:
//   - Required params: From, To, Url (instead of Body)
//   - SID prefix "CA"
//   - Stores Call (with TwiMLURL) instead of Message
//   - Renders make_call_*.json
//   - Schedules call status flow (queued → ringing → in-progress → completed)
func (s *Server) MakeCall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.logger.Warn("ParseForm failed", "error", err)
		writeJSONString(w, http.StatusBadRequest, `{"code":400,"message":"malformed request","status":400}`)
		return
	}

	accountSid := r.PathValue("AccountSid")
	req := provider.CallRequest{
		AccountSid:     accountSid,
		From:           r.PostFormValue("From"),
		To:             r.PostFormValue("To"),
		URL:            r.PostFormValue("Url"),
		StatusCallback: r.PostFormValue("StatusCallback"),
	}

	if err := s.provider.ValidateAuth(r.Header.Get("Authorization"), accountSid); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.provider.ValidateCall(req); err != nil {
		s.writeError(w, err)
		return
	}

	isKnown := s.provider.IsKnownNumber(req.To)
	willSucceed := false
	if isKnown {
		willSucceed = s.provider.ShouldSucceed(req.To)
	}

	sid := generateSID("CA")
	call := &storage.Call{
		SID: sid, Provider: s.provider.Name(),
		From: req.From, To: req.To,
		Status: "queued", CallbackURL: req.StatusCallback, TwiMLURL: req.URL,
	}
	if err := s.store.SaveCall(r.Context(), call); err != nil {
		s.logger.Error("SaveCall failed", "sid", sid, "error", err)
		writeJSONString(w, http.StatusInternalServerError,
			`{"code":500,"message":"storage error","status":500}`)
		return
	}

	body, err := s.tmpl.RenderCallResponse(tmpl.CallResponseData{
		CallSID:    sid,
		AccountSid: s.accountSid,
		Status:     "queued",
		Request: tmpl.CallRequestView{
			From: req.From, To: req.To, URL: req.URL,
		},
	}, true)
	if err != nil {
		s.logger.Error("render call response failed", "sid", sid, "error", err)
		writeJSONString(w, http.StatusInternalServerError,
			`{"code":500,"message":"render error","status":500}`)
		return
	}

	s.logger.Info("call created",
		"sid", sid, "from", req.From, "to", req.To,
		"is_known", isKnown, "will_succeed", willSucceed)

	writeJSONBytes(w, http.StatusCreated, body)

	cbURL := req.StatusCallback
	if !s.callbacksEnabled {
		cbURL = ""
	}
	s.dispatcher.ScheduleCallStatusFlow(sid, req.From, req.To, cbURL, isKnown, willSucceed)
}
