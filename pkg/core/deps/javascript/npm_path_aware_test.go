package javascript

import (
	"context"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

type fakeNPMFetcher struct {
	packages map[string]map[string]*deps.Package
	versions map[string][]string
}

func (f fakeNPMFetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	versions := f.versions[name]
	if len(versions) == 0 {
		return nil, nil
	}
	return f.FetchVersion(ctx, name, versions[len(versions)-1], refresh)
}

func (f fakeNPMFetcher) FetchVersion(_ context.Context, name, version string, _ bool) (*deps.Package, error) {
	pkg := f.packages[name][version]
	if pkg == nil {
		return nil, nil
	}
	cloned := *pkg
	if len(pkg.Dependencies) > 0 {
		cloned.Dependencies = append([]deps.Dependency(nil), pkg.Dependencies...)
	}
	return &cloned, nil
}

func (f fakeNPMFetcher) ListVersions(_ context.Context, name string, _ bool) ([]string, error) {
	return append([]string(nil), f.versions[name]...), nil
}

func (f fakeNPMFetcher) ListVersionsWithConstraints(_ context.Context, name string, _ bool) (map[string]string, error) {
	result := make(map[string]string, len(f.versions[name]))
	for _, version := range f.versions[name] {
		result[version] = ""
	}
	return result, nil
}

func TestNPMResolverAllowsDuplicateMajors(t *testing.T) {
	fake := fakeNPMFetcher{
		versions: map[string][]string{
			"nuxt":            {"1.0.0"},
			"@nuxt/cli":       {"3.35.1"},
			"@nuxt/telemetry": {"2.8.0"},
			"ofetch":          {"1.5.1", "2.0.0"},
		},
		packages: map[string]map[string]*deps.Package{
			"nuxt": {
				"1.0.0": {
					Name:    "nuxt",
					Version: "1.0.0",
					Dependencies: []deps.Dependency{
						{Name: "@nuxt/cli", Constraint: ">=3.35.1 <4.0.0"},
						{Name: "@nuxt/telemetry", Constraint: ">=2.8.0 <3.0.0"},
					},
				},
			},
			"@nuxt/cli": {
				"3.35.1": {
					Name:    "@nuxt/cli",
					Version: "3.35.1",
					Dependencies: []deps.Dependency{
						{Name: "ofetch", Constraint: ">=1.5.1 <2.0.0"},
					},
				},
			},
			"@nuxt/telemetry": {
				"2.8.0": {
					Name:    "@nuxt/telemetry",
					Version: "2.8.0",
					Dependencies: []deps.Dependency{
						{Name: "ofetch", Constraint: ">=2.0.0 <3.0.0"},
					},
				},
			},
			"ofetch": {
				"1.5.1": {Name: "ofetch", Version: "1.5.1"},
				"2.0.0": {Name: "ofetch", Version: "2.0.0"},
			},
		},
	}

	resolver := &npmResolver{fetcher: fake, matcher: SemverMatcher{}}

	g, err := resolver.Resolve(context.Background(), "nuxt", deps.Options{MaxDepth: 10, MaxNodes: 100})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// The greedy resolver should produce ofetch@1.5.1 and ofetch@2.0.0 as
	// separate graph nodes since two versions coexist.
	if _, ok := g.Node("ofetch@1.5.1"); !ok {
		t.Error("expected ofetch@1.5.1 node in graph")
	}
	if _, ok := g.Node("ofetch@2.0.0"); !ok {
		t.Error("expected ofetch@2.0.0 node in graph")
	}
	// Single-version packages should use bare names
	if _, ok := g.Node("nuxt"); !ok {
		t.Error("expected bare 'nuxt' node (not nuxt@1.0.0)")
	}
	if _, ok := g.Node("@nuxt/cli"); !ok {
		t.Error("expected bare '@nuxt/cli' node")
	}
}

func TestNPMResolverDeduplicatesCompatibleVersions(t *testing.T) {
	fake := fakeNPMFetcher{
		versions: map[string][]string{
			"app":    {"1.0.0"},
			"a":      {"1.0.0"},
			"b":      {"1.0.0"},
			"lodash": {"4.17.21"},
		},
		packages: map[string]map[string]*deps.Package{
			"app": {"1.0.0": {Name: "app", Version: "1.0.0", Dependencies: []deps.Dependency{
				{Name: "a", Constraint: "^1.0.0"},
				{Name: "b", Constraint: "^1.0.0"},
			}}},
			"a": {"1.0.0": {Name: "a", Version: "1.0.0", Dependencies: []deps.Dependency{
				{Name: "lodash", Constraint: "^4.17.0"},
			}}},
			"b": {"1.0.0": {Name: "b", Version: "1.0.0", Dependencies: []deps.Dependency{
				{Name: "lodash", Constraint: "^4.17.0"},
			}}},
			"lodash": {"4.17.21": {Name: "lodash", Version: "4.17.21"}},
		},
	}

	resolver := &npmResolver{fetcher: fake, matcher: SemverMatcher{}}

	g, err := resolver.Resolve(context.Background(), "app", deps.Options{MaxDepth: 10, MaxNodes: 100})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// lodash should use bare name since only one version resolved
	if _, ok := g.Node("lodash"); !ok {
		t.Error("expected bare 'lodash' node")
	}
	if _, ok := g.Node("lodash@4.17.21"); ok {
		t.Error("expected bare 'lodash', not 'lodash@4.17.21'")
	}
}
