//go:build !linux

package agent

import (
	"net/http"
)

// handleShell is a stub on non-Linux hosts. The guest agent binary is always
// built for Linux; this stub lets agent package tests compile and run on macOS CI.
func (s *Server) handleShell(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "interactive shell requires linux guest agent", http.StatusNotImplemented)
}
