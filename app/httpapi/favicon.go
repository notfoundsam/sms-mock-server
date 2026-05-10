package httpapi

import "net/http"

// Favicon handles GET /favicon.ico by redirecting to the embedded SVG
// asset. Browsers fetch /favicon.ico unconditionally regardless of any
// <link rel="icon"> in the page, so we serve a redirect rather than
// returning 404.
func (s *Server) Favicon(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/static/img/icon.svg", http.StatusMovedPermanently)
}
