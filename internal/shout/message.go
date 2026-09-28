package shout

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/vmihailenco/msgpack/v5"
)

type ShoutMessage struct {
	DeviceID     string `json:"device_id" msgpack:"device_id"`
	DeviceName   string `json:"device_name" msgpack:"device_name"`
	SessionID    string `json:"session_id" msgpack:"session_id"`
	InterfaceIP  string `json:"interface_ip" msgpack:"interface_ip"`
	TransferPort int    `json:"transfer_port" msgpack:"transfer_port"`
}

func (m *ShoutMessage) Validate() error {
	if m == nil {
		return fmt.Errorf("shout: message is nil")
	}
	if m.DeviceID == "" {
		return fmt.Errorf("shout: device_id is required")
	}
	if m.DeviceName == "" {
		return fmt.Errorf("shout: device_name is required")
	}
	if m.SessionID == "" {
		return fmt.Errorf("shout: session_id is required")
	}
	if net.ParseIP(m.InterfaceIP) == nil {
		return fmt.Errorf("shout: invalid interface_ip: %q", m.InterfaceIP)
	}
	if m.TransferPort <= 0 || m.TransferPort > 65535 {
		return fmt.Errorf("shout: invalid transfer_port: %d", m.TransferPort)
	}
	return nil
}

func (m *ShoutMessage) EncodeMsgpack() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return msgpack.Marshal(m)
}

func DecodeMsgpack(data []byte) (*ShoutMessage, error) {
	var m ShoutMessage
	if err := msgpack.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("shout: decode msgpack: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *ShoutMessage) EncodeJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func DecodeJSON(data []byte) (*ShoutMessage, error) {
	var m ShoutMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("shout: decode json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
