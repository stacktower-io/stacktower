//go:build dev

package cli

import (
	"testing"
)

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{
			name:  "two indices",
			input: "0,1",
			want:  []int{0, 1},
		},
		{
			name:  "three indices",
			input: "0,1,2",
			want:  []int{0, 1, 2},
		},
		{
			name:  "with spaces",
			input: "0, 1, 2",
			want:  []int{0, 1, 2},
		},
		{
			name:  "larger numbers",
			input: "10,20,30",
			want:  []int{10, 20, 30},
		},
		{
			name:    "single index",
			input:   "0",
			wantErr: true,
		},
		{
			name:    "invalid index",
			input:   "0,a",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "just comma",
			input:   ",",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConstraint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConstraint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("parseConstraint(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("parseConstraint(%q)[%d] = %d, want %d", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}
