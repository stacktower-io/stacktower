package deps

import (
	"context"
	"errors"
	"testing"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
)

var _ ManifestParser = (*mockManifestParser)(nil)

type mockResolver struct {
	name string
}

func (m *mockResolver) Name() string { return m.name }
func (m *mockResolver) Resolve(ctx context.Context, pkg string, opts Options) (*dag.DAG, error) {
	return dag.New(nil), nil
}

type mockManifestParser struct {
	typeName string
}

func (m *mockManifestParser) Type() string                  { return m.typeName }
func (m *mockManifestParser) Supports(filename string) bool { return true }
func (m *mockManifestParser) IncludesTransitive() bool      { return false }
func (m *mockManifestParser) Parse(path string, opts Options) (*ManifestResult, error) {
	return &ManifestResult{}, nil
}

func TestLanguageRegistry(t *testing.T) {
	lang := &Language{
		Name:            "test",
		DefaultRegistry: "default-reg",
		RegistryAliases: map[string]string{"alias": "default-reg"},
		NewResolver: func(c cache.Cache, opts Options) (Resolver, error) {
			return &mockResolver{name: "default-reg"}, nil
		},
	}

	tests := []struct {
		name       string
		registry   string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "default registry",
			registry: "default-reg",
			wantErr:  false,
		},
		{
			name:     "aliased registry",
			registry: "alias",
			wantErr:  false,
		},
		{
			name:       "unknown registry",
			registry:   "unknown",
			wantErr:    true,
			wantErrMsg: `unknown registry "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := lang.Registry(cache.NewNullCache(), tt.registry, Options{})
			if tt.wantErr {
				if err == nil {
					t.Error("Registry() expected error, got nil")
				} else if tt.wantErrMsg != "" && !contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("Registry() error = %q, want containing %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("Registry() unexpected error: %v", err)
			}
			if res == nil {
				t.Error("Registry() returned nil resolver")
			}
		})
	}
}

func TestLanguageRegistryError(t *testing.T) {
	expectedErr := errors.New("resolver creation failed")
	lang := &Language{
		Name:            "test",
		DefaultRegistry: "default-reg",
		NewResolver: func(c cache.Cache, opts Options) (Resolver, error) {
			return nil, expectedErr
		},
	}

	_, err := lang.Registry(cache.NewNullCache(), "default-reg", Options{})
	if err != expectedErr {
		t.Errorf("Registry() error = %v, want %v", err, expectedErr)
	}
}

func TestLanguageResolver(t *testing.T) {
	lang := &Language{
		Name:            "test",
		DefaultRegistry: "default-reg",
		NewResolver: func(c cache.Cache, opts Options) (Resolver, error) {
			return &mockResolver{name: "default-reg"}, nil
		},
	}

	res, err := lang.Resolver(cache.NewNullCache(), Options{})
	if err != nil {
		t.Errorf("Resolver() unexpected error: %v", err)
	}
	if res == nil {
		t.Error("Resolver() returned nil")
	}
	if res.Name() != "default-reg" {
		t.Errorf("Resolver().Name() = %q, want %q", res.Name(), "default-reg")
	}
}

func TestLanguageManifest(t *testing.T) {
	lang := &Language{
		Name:            "test",
		ManifestTypes:   []string{"poetry", "requirements"},
		ManifestAliases: map[string]string{"pyproject.toml": "poetry", "requirements.txt": "requirements"},
		NewManifest: func(name string, res Resolver) ManifestParser {
			if name == "poetry" || name == "requirements" {
				return &mockManifestParser{typeName: name}
			}
			return nil
		},
	}

	resolver := &mockResolver{name: "pypi"}

	tests := []struct {
		name         string
		manifestName string
		wantOK       bool
		wantType     string
	}{
		{
			name:         "direct manifest type",
			manifestName: "poetry",
			wantOK:       true,
			wantType:     "poetry",
		},
		{
			name:         "aliased manifest type",
			manifestName: "pyproject.toml",
			wantOK:       true,
			wantType:     "poetry",
		},
		{
			name:         "another manifest type",
			manifestName: "requirements.txt",
			wantOK:       true,
			wantType:     "requirements",
		},
		{
			name:         "unknown manifest type",
			manifestName: "unknown",
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, ok := lang.Manifest(tt.manifestName, resolver)
			if ok != tt.wantOK {
				t.Errorf("Manifest() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK {
				if parser == nil {
					t.Error("Manifest() returned nil parser when ok=true")
				} else if parser.Type() != tt.wantType {
					t.Errorf("Manifest().Type() = %q, want %q", parser.Type(), tt.wantType)
				}
			}
		})
	}
}

func TestLanguageManifestNilFactory(t *testing.T) {
	lang := &Language{
		Name:        "test",
		NewManifest: nil,
	}

	parser, ok := lang.Manifest("anything", nil)
	if ok {
		t.Error("Manifest() should return false when NewManifest is nil")
	}
	if parser != nil {
		t.Error("Manifest() should return nil parser when NewManifest is nil")
	}
}

func TestLanguageHasManifests(t *testing.T) {
	tests := []struct {
		name string
		lang *Language
		want bool
	}{
		{
			name: "has manifests",
			lang: &Language{
				NewManifest: func(name string, res Resolver) ManifestParser {
					return &mockManifestParser{typeName: "test"}
				},
			},
			want: true,
		},
		{
			name: "no manifests",
			lang: &Language{
				NewManifest: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lang.HasManifests(); got != tt.want {
				t.Errorf("HasManifests() = %v, want %v", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
