package constraints

import "testing"

func TestCheckVersionConstraint(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		constraint string
		want       bool
	}{
		// Basic operators
		{"empty constraint", "1.0.0", "", true},
		{">=3.8 with 3.11", "3.11", ">=3.8", true},
		{">=3.8 with 3.7", "3.7", ">=3.8", false},
		{"<4.0 with 3.9", "3.9", "<4.0", true},
		{"<4.0 with 4.0", "4.0", "<4.0", false},
		{"combined AND", "3.9", ">=3.8,<4", true},
		{"combined AND fail", "4.0", ">=3.8,<4", false},

		// Caret
		{"caret", "3.11", "^3.10", true},
		{"caret fail", "4.0", "^3.10", false},
		{"caret 0.x compatible", "0.8.5", "^0.8", true},
		{"caret 0.x incompatible", "0.9.0", "^0.8", false},

		// Tilde
		{"tilde", "3.10.5", "~3.10", true},
		{"tilde fail", "3.11", "~3.10", false},
		{"tilde ruby", "2.7.8", "~>2.7", true},
		{"tilde ruby fail", "3.0", "~>2.7", false},

		// OR groups (Composer-style)
		{"composer OR", "8.2", "^8.2|^8.3|^8.4|^8.5", true},
		{"composer OR fail", "9.0", "^8.2|^8.3|^8.4|^8.5", false},
		{"composer OR with double pipes", "8.2", "^8.1 || ^8.2", true},

		// Bare versions normalized to >=
		{"bare version normalized", "1.75", "1.70", true},
		{"bare version normalized lower", "1.69", "1.70", false},

		// Unknown operator fails closed
		{"unknown operator fails closed", "3.11", "!~3.10", false},

		// Equality
		{"equal match", "3.11.0", "==3.11.0", true},
		{"equal mismatch", "3.11.1", "==3.11.0", false},
		{"not-equal match", "3.12", "!=3.11", true},
		{"not-equal mismatch", "3.11", "!=3.11", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckVersionConstraint(tt.version, tt.constraint)
			if got != tt.want {
				t.Fatalf("CheckVersionConstraint(%q, %q) = %v, want %v", tt.version, tt.constraint, got, tt.want)
			}
		})
	}
}

func TestExtractMinVersion(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{"", ""},
		{">=3.8", "3.8"},
		{"^3.10", "3.10"},
		{"~3.9", "3.9"},
		{">=3.8,<4", "3.8"},
		{"<4", ""},
		{"^8.2|^8.3|^8.4|^8.5", "8.2"},
		{"1.70.0", "1.70.0"},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got := ExtractMinVersion(tt.constraint)
			if got != tt.want {
				t.Fatalf("ExtractMinVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestNormalizeRuntimeConstraint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"1.21", ">=1.21"},
		{">=3.10", ">=3.10"},
		{"^8.2|^8.3", "^8.2|^8.3"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := NormalizeRuntimeConstraint(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeRuntimeConstraint(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
