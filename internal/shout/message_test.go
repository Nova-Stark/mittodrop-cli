package shout

import (
	"testing"
)

func validMessage() *ShoutMessage {
	return &ShoutMessage{
		DeviceID:     "1672650e-f131-5da8-a973-d98f81d0ae0c",
		DeviceName:   "peacefulsculpture918",
		SessionID:    "e6c1e34c-6fa4-4903-b26a-93fba2f2ff5a",
		InterfaceIP:  "192.168.1.15",
		TransferPort: 49201,
	}
}

func TestShoutMessage_MsgpackRoundtrip(t *testing.T) {
	orig := validMessage()
	data, err := orig.EncodeMsgpack()
	if err != nil {
		t.Fatalf("encode msgpack failed: %v", err)
	}

	decoded, err := DecodeMsgpack(data)
	if err != nil {
		t.Fatalf("decode msgpack failed: %v", err)
	}

	if *decoded != *orig {
		t.Errorf("mismatch:\ngot  %+v\nwant %+v", *decoded, *orig)
	}

	t.Logf("Msgpack encoded size: %d bytes", len(data))
}

func TestShoutMessage_JSONRoundtrip(t *testing.T) {
	orig := validMessage()
	data, err := orig.EncodeJSON()
	if err != nil {
		t.Fatalf("encode json failed: %v", err)
	}

	decoded, err := DecodeJSON(data)
	if err != nil {
		t.Fatalf("decode json failed: %v", err)
	}

	if *decoded != *orig {
		t.Errorf("mismatch:\ngot  %+v\nwant %+v", *decoded, *orig)
	}

	t.Logf("JSON encoded size: %d bytes", len(data))
}

func TestShoutMessage_Validation(t *testing.T) {
	tests := []struct {
		name    string
		msg     ShoutMessage
		wantErr bool
	}{
		{
			name: "missing device_id",
			msg: ShoutMessage{
				DeviceName:   "dev",
				SessionID:    "sess",
				InterfaceIP:  "192.168.1.1",
				TransferPort: 49201,
			},
			wantErr: true,
		},
		{
			name: "invalid ip",
			msg: ShoutMessage{
				DeviceID:     "dev-id",
				DeviceName:   "dev",
				SessionID:    "sess",
				InterfaceIP:  "not-an-ip",
				TransferPort: 49201,
			},
			wantErr: true,
		},
		{
			name: "invalid port zero",
			msg: ShoutMessage{
				DeviceID:     "dev-id",
				DeviceName:   "dev",
				SessionID:    "sess",
				InterfaceIP:  "192.168.1.1",
				TransferPort: 0,
			},
			wantErr: true,
		},
		{
			name: "valid ipv6 message",
			msg: ShoutMessage{
				DeviceID:     "dev-id",
				DeviceName:   "dev",
				SessionID:    "sess",
				InterfaceIP:  "fe80::1",
				TransferPort: 49205,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeMsgpack_RejectsGarbage(t *testing.T) {
	garbage := []byte("random noise from other app")
	_, err := DecodeMsgpack(garbage)
	if err == nil {
		t.Error("expected error decoding garbage bytes, got nil")
	}
}
