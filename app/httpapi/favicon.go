package httpapi

import "net/http"

// faviconSVG is the inline SMS-bubble icon, byte-for-byte the same as
// `app/main.py:177-184` so callers receiving the favicon see no change.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
        <rect width="100" height="100" fill="#3498db"/>
        <path d="M20 25 h60 a8 8 0 0 1 8 8 v30 a8 8 0 0 1 -8 8 h-40 l-15 15 v-15 h-5 a8 8 0 0 1 -8 -8 v-30 a8 8 0 0 1 8 -8 z"
              fill="#ffffff" stroke="#ffffff" stroke-width="2"/>
        <circle cx="35" cy="45" r="3" fill="#3498db"/>
        <circle cx="50" cy="45" r="3" fill="#3498db"/>
        <circle cx="65" cy="45" r="3" fill="#3498db"/>
    </svg>`

// Favicon handles GET /favicon.ico, returning an inline SVG icon.
func (s *Server) Favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(faviconSVG))
}
