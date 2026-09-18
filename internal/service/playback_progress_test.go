package service

import "testing"

func TestPlaybackProgressRules(t *testing.T) {
	tests := []struct {
		name      string
		position  int64
		duration  int64
		wantError bool
		completed bool
	}{
		{name: "negative position", position: -1, duration: 60_000, wantError: true},
		{name: "zero duration", position: 0, duration: 0, wantError: true},
		{name: "past duration", position: 60_001, duration: 60_000, wantError: true},
		{name: "short incomplete", position: 569_998, duration: 599_999},
		{name: "short complete", position: 569_999, duration: 599_999, completed: true},
		{name: "ten minute incomplete", position: 539_999, duration: 600_000},
		{name: "ten minute complete", position: 540_000, duration: 600_000, completed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlaybackProgress(tt.position, tt.duration)
			if (err != nil) != tt.wantError {
				t.Fatalf("validatePlaybackProgress() error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil && playbackCompleted(tt.position, tt.duration) != tt.completed {
				t.Fatalf("playbackCompleted() = %v, want %v", playbackCompleted(tt.position, tt.duration), tt.completed)
			}
		})
	}
}

func TestPlaybackProgressRecordingThreshold(t *testing.T) {
	tests := []struct {
		name     string
		position int64
		duration int64
		want     bool
	}{
		{name: "short before threshold", position: 19_999, duration: 599_999},
		{name: "short at threshold", position: 20_000, duration: 599_999, want: true},
		{name: "ten minutes before threshold", position: 19_999, duration: 600_000},
		{name: "ten minutes at threshold", position: 20_000, duration: 600_000, want: true},
		{name: "long at old threshold", position: 20_000, duration: 600_001},
		{name: "long before threshold", position: 59_999, duration: 600_001},
		{name: "long at threshold", position: 60_000, duration: 600_001, want: true},
		{name: "long after threshold", position: 60_001, duration: 600_001, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRecordPlaybackProgress(tt.position, tt.duration); got != tt.want {
				t.Fatalf("shouldRecordPlaybackProgress(%d, %d) = %v, want %v", tt.position, tt.duration, got, tt.want)
			}
		})
	}
}
