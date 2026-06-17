package uuidutil

import "testing"

func TestIsUUIDString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "canonical",
			value: "00000000-0000-4000-8000-000000000001",
			want:  true,
		},
		{
			name:  "trimmed",
			value: " 00000000-0000-4000-8000-000000000001 ",
			want:  true,
		},
		{
			name:  "missing_dashes",
			value: "00000000000040008000000000000001",
			want:  false,
		},
		{
			name:  "unsafe_character",
			value: "00000000-0000-4000-8000-00000000000g",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUUIDString(tt.value); got != tt.want {
				t.Fatalf("IsUUIDString(%q)=%v want %v", tt.value, got, tt.want)
			}
		})
	}
}
