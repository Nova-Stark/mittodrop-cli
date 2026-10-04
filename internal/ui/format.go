package ui

import (
	"fmt"
	"time"
)

// FormatBytes converts raw bytes into human-readable format (B, KB, MB, GB).
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}

// FormatSpeed converts bytes per second into human-readable speed string (e.g. 24.5 MB/s).
func FormatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 B/s"
	}
	const unit = 1024
	if bytesPerSec < unit {
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
	div, exp := float64(unit), 0
	for n := bytesPerSec / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB/s", "MB/s", "GB/s", "TB/s"}
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %s", bytesPerSec/div, units[exp])
}

// FormatDuration converts duration into clean compact ETA string (e.g. 3s, 1m 05s).
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	sec := int64(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	remSec := sec % 60
	if min < 60 {
		return fmt.Sprintf("%dm %02ds", min, remSec)
	}
	hr := min / 60
	remMin := min % 60
	return fmt.Sprintf("%dh %02dm", hr, remMin)
}

// RenderBar renders an ASCII progress bar (e.g. [=======>    ]).
func RenderBar(percent float64, width int) string {
	if width < 5 {
		width = 20
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int((percent / 100.0) * float64(width))
	if filled > width {
		filled = width
	}

	b := make([]byte, width)
	for i := 0; i < filled; i++ {
		if i == filled-1 && filled < width {
			b[i] = '>'
		} else {
			b[i] = '='
		}
	}
	for i := filled; i < width; i++ {
		b[i] = ' '
	}

	return fmt.Sprintf("[%s]", string(b))
}
