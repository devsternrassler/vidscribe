package pipeline

import "testing"

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "0:00"},
		{59, "0:59"},
		{60, "1:00"},
		{90, "1:30"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{7322, "2:02:02"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.seconds)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"20240115", "2024-01-15"},
		{"20000101", "2000-01-01"},
		{"", ""},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		got := formatDate(tt.input)
		if got != tt.want {
			t.Errorf("formatDate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChannelName(t *testing.T) {
	tests := []struct {
		meta Metadata
		want string
	}{
		{Metadata{Channel: "Chan", Uploader: "Up"}, "Chan"},
		{Metadata{Channel: "", Uploader: "Up"}, "Up"},
		{Metadata{}, ""},
	}

	for _, tt := range tests {
		got := channelName(&tt.meta)
		if got != tt.want {
			t.Errorf("channelName(%+v) = %q, want %q", tt.meta, got, tt.want)
		}
	}
}
