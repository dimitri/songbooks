package httpapi

import (
	"net/http"

	qrcode "github.com/skip2/go-qrcode"
)

// handleQRCode renders a PNG QR code for ?data=<url> — used next to any
// invite/join link so it can be scanned straight off a phone screen
// (sharing a link via WhatsApp/contacts isn't always the easiest path on
// a crowded rehearsal-room phone). data is whatever the page already
// shows as the copyable link; this never looks it up itself, so it can't
// leak anything the visitor didn't already have on screen.
func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	data := r.URL.Query().Get("data")
	if data == "" {
		httpError(w, http.StatusBadRequest, "missing data")
		return
	}
	png, err := qrcode.Encode(data, qrcode.Medium, 320)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}
