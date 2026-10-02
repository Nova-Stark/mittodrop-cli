package utils

// PeerIdentity represents identity descriptors for a mittodrop node.
type PeerIdentity struct {
	DeviceID   string `json:"device_id" msgpack:"device_id"`     // Hardware-based UUID (MAC + Hostname + OS)
	DeviceName string `json:"device_name" msgpack:"device_name"` // Human-friendly callsign (e.g. SwiftFalcon402)
	SessionID  string `json:"session_id" msgpack:"session_id"`   // Unique UUIDv7 per transfer instance
}

// EnsureValid ensures all fields of PeerIdentity are non-empty.
func (id *PeerIdentity) EnsureValid(defaultName string) error {
	if id.DeviceID == "" {
		devID, err := GenerateDeviceID()
		if err != nil {
			return err
		}
		id.DeviceID = devID
	}
	if id.SessionID == "" {
		sessID, err := GenerateSessionID()
		if err != nil {
			return err
		}
		id.SessionID = sessID
	}
	if id.DeviceName == "" {
		if defaultName != "" {
			id.DeviceName = defaultName
		} else {
			id.DeviceName = id.DeviceID[:8]
		}
	}
	return nil
}

// PeerHello is the authenticated identity envelope exchanged during connection handshake.
type PeerHello struct {
	Magic    string       `json:"magic"` // "mittodrop-auth" or "mittodrop-auth-ack"
	Identity PeerIdentity `json:"identity"`
}
