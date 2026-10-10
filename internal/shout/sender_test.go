package shout

import (
	"context"
	"testing"
	"time"
)

func TestNewSender_Validation(t *testing.T) {
	validCfg := SenderConfig{
		DeviceID:     "dev-1",
		DeviceName:   "friendly-device",
		SessionID:    "sess-1",
		TransferPort: 49201,
	}

	tests := []struct {
		name    string
		modify  func(cfg *SenderConfig)
		wantErr bool
	}{
		{
			name:    "valid defaults to 1500ms",
			modify:  func(cfg *SenderConfig) {},
			wantErr: false,
		},
		{
			name: "missing device_id",
			modify: func(cfg *SenderConfig) {
				cfg.DeviceID = ""
			},
			wantErr: true,
		},
		{
			name: "missing device_name",
			modify: func(cfg *SenderConfig) {
				cfg.DeviceName = ""
			},
			wantErr: true,
		},
		{
			name: "missing session_id",
			modify: func(cfg *SenderConfig) {
				cfg.SessionID = ""
			},
			wantErr: true,
		},
		{
			name: "invalid transfer port",
			modify: func(cfg *SenderConfig) {
				cfg.TransferPort = 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validCfg
			tt.modify(&cfg)
			s, err := NewSender(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewSender() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && s.cfg.Interval != DefaultShoutInterval {
				t.Errorf("expected default interval %v, got %v", DefaultShoutInterval, s.cfg.Interval)
			}
		})
	}
}

func TestSender_StartContextCancel(t *testing.T) {
	s, err := NewSender(SenderConfig{
		DeviceID:     "dev-1",
		DeviceName:   "friendly-device",
		SessionID:    "sess-1",
		TransferPort: 49201,
		Interval:     50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	err = s.Start(ctx)
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("expected context cancellation error, got %v", err)
	}
}

func TestSender_ShoutOnceRunsWithoutPanic(t *testing.T) {
	s, err := NewSender(SenderConfig{
		DeviceID:     "test-dev",
		DeviceName:   "test-name",
		SessionID:    "test-sess",
		TransferPort: 49201,
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	// Should execute across all system network interfaces without panicking
	s.ShoutOnce()
}
