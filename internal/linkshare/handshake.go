package linkshare

import (
	"encoding/json"
	"net/http"
)

type HandshakeResponse struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	SessionID  string `json:"session_id"`
	Status     string `json:"status"`
}

func (r *Receiver) handleHandshake(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := HandshakeResponse{
		DeviceID:   r.cfg.DeviceID,
		DeviceName: r.cfg.DeviceName,
		SessionID:  r.cfg.SessionID,
		Status:     "ready",
	}

	w.Header().Set("Content-Type", "application/json")
	if req.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}
