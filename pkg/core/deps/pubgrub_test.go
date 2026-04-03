package deps

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contriboss/pubgrub-go"
)

// mockParser implements ConstraintParser for testing
type mockParser struct{}

func (mockParser) ParseVersion(v string) pubgrub.Version {
	sv, err := pubgrub.ParseSemanticVersion(v)
	if err != nil {
		return pubgrub.SimpleVersion(v)
	}
	return sv
}

func (mockParser) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}
	vs, err := pubgrub.ParseVersionRange(constraint)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(vs)
}

// mockVersionLister implements both Fetcher and VersionLister
type mockVersionLister struct {
	packages           map[string]map[string]*Package // name -> version -> package
	versionConstraints map[string]map[string]string   // name -> version -> runtime constraint
}

func (m *mockVersionLister) Fetch(ctx context.Context, name string, refresh bool) (*Package, error) {
	// Return latest version
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	// Find latest (simple string sort for mock)
	var latest *Package
	for _, pkg := range versions {
		if latest == nil || pkg.Version > latest.Version {
			latest = pkg
		}
	}
	return latest, nil
}

func (m *mockVersionLister) FetchVersion(ctx context.Context, name, version string, refresh bool) (*Package, error) {
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	pkg, ok := versions[version]
	if !ok {
		return nil, ErrNotFound
	}
	return pkg, nil
}

func (m *mockVersionLister) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	result := make([]string, 0, len(versions))
	for v := range versions {
		result = append(result, v)
	}
	return result, nil
}

func (m *mockVersionLister) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	if m.versionConstraints == nil {
		return nil, nil
	}
	constraints, ok := m.versionConstraints[name]
	if !ok {
		return nil, ErrNotFound
	}
	out := make(map[string]string, len(constraints))
	for version, constraint := range constraints {
		out[version] = constraint
	}
	return out, nil
}

func TestPubGrubResolver_Resolve(t *testing.T) {
	// Create a mock package registry
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"1.0.0": {
					Name:    "root",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "dep-a", Constraint: ">=1.0.0, <2.0.0"},
						{Name: "dep-b", Constraint: ">=2.0.0"},
					},
				},
			},
			"dep-a": {
				"1.0.0": {
					Name:    "dep-a",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "shared", Constraint: ">=1.0.0, <1.5.0"},
					},
				},
				"1.5.0": {
					Name:    "dep-a",
					Version: "1.5.0",
					Dependencies: []Dependency{
						{Name: "shared", Constraint: ">=1.0.0, <2.0.0"},
					},
				},
			},
			"dep-b": {
				"2.0.0": {
					Name:    "dep-b",
					Version: "2.0.0",
					Dependencies: []Dependency{
						{Name: "shared", Constraint: ">=1.2.0"},
					},
				},
			},
			"shared": {
				"1.0.0": {Name: "shared", Version: "1.0.0"},
				"1.2.0": {Name: "shared", Version: "1.2.0"},
				"1.4.0": {Name: "shared", Version: "1.4.0"},
				"2.0.0": {Name: "shared", Version: "2.0.0"},
			},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	ctx := context.Background()
	dag, err := resolver.Resolve(ctx, "root", Options{Version: "1.0.0"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify all packages are resolved
	expectedPackages := []string{"root", "dep-a", "dep-b", "shared"}
	for _, pkg := range expectedPackages {
		if _, ok := dag.Node(pkg); !ok {
			t.Errorf("expected package %s in solution", pkg)
		}
	}

	// Verify shared version is compatible with all constraints
	// dep-a@1.5.0 requires shared >=1.0.0, <2.0.0
	// dep-b@2.0.0 requires shared >=1.2.0
	// The solver should pick shared@1.4.0 (highest compatible)
	if sharedNode, ok := dag.Node("shared"); ok {
		if sharedNode.Meta != nil {
			version := sharedNode.Meta["version"]
			// The version should be in the range [1.2.0, 2.0.0)
			t.Logf("Resolved shared version: %v", version)
		}
	}
}

func TestPubGrubResolver_ConflictDetection(t *testing.T) {
	// Create a package registry with an unsolvable conflict
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"1.0.0": {
					Name:    "root",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "dep-a", Constraint: ">=1.0.0"},
						{Name: "dep-b", Constraint: ">=1.0.0"},
					},
				},
			},
			"dep-a": {
				"1.0.0": {
					Name:    "dep-a",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "conflict", Constraint: ">=2.0.0"}, // Requires >=2.0.0
					},
				},
			},
			"dep-b": {
				"1.0.0": {
					Name:    "dep-b",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "conflict", Constraint: "<2.0.0"}, // Requires <2.0.0
					},
				},
			},
			"conflict": {
				"1.0.0": {Name: "conflict", Version: "1.0.0"},
				"2.0.0": {Name: "conflict", Version: "2.0.0"},
			},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	ctx := context.Background()
	_, err = resolver.Resolve(ctx, "root", Options{Version: "1.0.0"})

	// Should fail with conflict
	if err == nil {
		t.Error("expected resolution to fail due to conflict")
	} else {
		t.Logf("Conflict detected (expected): %v", err)
	}
}

func TestPubGrubResolver_RespectsRootConstraint(t *testing.T) {
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"4.18.0": {
					Name:    "root",
					Version: "4.18.0",
					Dependencies: []Dependency{
						{Name: "child", Constraint: ">=1.0.0, <2.0.0"},
					},
				},
				"5.2.1": {
					Name:    "root",
					Version: "5.2.1",
					Dependencies: []Dependency{
						{Name: "child", Constraint: ">=2.0.0, <3.0.0"},
					},
				},
			},
			"child": {
				"1.9.0": {Name: "child", Version: "1.9.0"},
				"2.1.0": {Name: "child", Version: "2.1.0"},
			},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	g, err := resolver.Resolve(context.Background(), "root", Options{Constraint: ">=4.18.0, <5.0.0"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	rootNode, ok := g.Node("root")
	if !ok {
		t.Fatalf("expected root node in graph")
	}
	if got := rootNode.Meta["version"]; got != "4.18.0" {
		t.Fatalf("root version = %v, want 4.18.0", got)
	}
}

func TestPubGrubResolver_FiltersRuntimeIncompatibleVersions(t *testing.T) {
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"1.0.0": {
					Name:    "root",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "dep", Constraint: ">=1.0.0"},
					},
				},
			},
			"dep": {
				"1.0.0": {Name: "dep", Version: "1.0.0"},
				"2.0.0": {Name: "dep", Version: "2.0.0"},
			},
		},
		versionConstraints: map[string]map[string]string{
			"dep": {
				"1.0.0": ">=3.8,<3.10",
				"2.0.0": ">=3.10",
			},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	g, err := resolver.Resolve(context.Background(), "root", Options{
		Version:        "1.0.0",
		RuntimeVersion: "3.9",
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	depNode, ok := g.Node("dep")
	if !ok {
		t.Fatalf("expected dep node")
	}
	if got := depNode.Meta["version"]; got != "1.0.0" {
		t.Fatalf("dep version = %v, want 1.0.0", got)
	}
}

func TestPubGrubResolver_RespectsMaxDepth(t *testing.T) {
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {"1.0.0": {Name: "root", Version: "1.0.0", Dependencies: []Dependency{{Name: "a", Constraint: "1.0.0"}}}},
			"a":    {"1.0.0": {Name: "a", Version: "1.0.0", Dependencies: []Dependency{{Name: "b", Constraint: "1.0.0"}}}},
			"b":    {"1.0.0": {Name: "b", Version: "1.0.0", Dependencies: []Dependency{{Name: "c", Constraint: "1.0.0"}}}},
			"c":    {"1.0.0": {Name: "c", Version: "1.0.0"}},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	g, err := resolver.Resolve(context.Background(), "root", Options{Version: "1.0.0", MaxDepth: 2})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	for _, keep := range []string{"root", "a", "b"} {
		if _, ok := g.Node(keep); !ok {
			t.Fatalf("expected node %q", keep)
		}
	}
	if _, ok := g.Node("c"); ok {
		t.Fatalf("did not expect node %q at depth > MaxDepth", "c")
	}
}

func TestPubGrubResolver_RespectsMaxNodesDuringExpansion(t *testing.T) {
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"1.0.0": {
					Name:    "root",
					Version: "1.0.0",
					Dependencies: []Dependency{
						{Name: "a", Constraint: "1.0.0"},
						{Name: "b", Constraint: "1.0.0"},
						{Name: "c", Constraint: "1.0.0"},
					},
				},
			},
			"a": {"1.0.0": {Name: "a", Version: "1.0.0"}},
			"b": {"1.0.0": {Name: "b", Version: "1.0.0"}},
			"c": {"1.0.0": {Name: "c", Version: "1.0.0"}},
		},
	}

	resolver, err := NewPubGrubResolver("test", fetcher, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	g, err := resolver.Resolve(context.Background(), "root", Options{Version: "1.0.0", MaxNodes: 2})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if g.NodeCount() > 2 {
		t.Fatalf("node count = %d, want <= 2", g.NodeCount())
	}
	if _, ok := g.Node("root"); !ok {
		t.Fatal("expected root node")
	}
}

// ErrNotFound for testing
var ErrNotFound = errors.New("package not found")

type countingVersionLister struct {
	packages map[string]map[string]*Package
	delay    time.Duration
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func (m *countingVersionLister) Fetch(ctx context.Context, name string, refresh bool) (*Package, error) {
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	for _, pkg := range versions {
		return pkg, nil
	}
	return nil, ErrNotFound
}

func (m *countingVersionLister) FetchVersion(ctx context.Context, name, version string, refresh bool) (*Package, error) {
	cur := m.inFlight.Add(1)
	for {
		prev := m.maxSeen.Load()
		if cur <= prev || m.maxSeen.CompareAndSwap(prev, cur) {
			break
		}
	}
	defer m.inFlight.Add(-1)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	pkg, ok := versions[version]
	if !ok {
		return nil, ErrNotFound
	}
	return pkg, nil
}

func (m *countingVersionLister) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	versions, ok := m.packages[name]
	if !ok {
		return nil, ErrNotFound
	}
	result := make([]string, 0, len(versions))
	for v := range versions {
		result = append(result, v)
	}
	return result, nil
}

func TestPubGrubResolver_solutionToDAG_ParallelPackageFetchBounded(t *testing.T) {
	fetcher := &countingVersionLister{
		delay: 35 * time.Millisecond,
		packages: map[string]map[string]*Package{
			"root": {"1.0.0": {Name: "root", Version: "1.0.0", Dependencies: []Dependency{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}}},
			"a":    {"1.0.0": {Name: "a", Version: "1.0.0"}},
			"b":    {"1.0.0": {Name: "b", Version: "1.0.0"}},
			"c":    {"1.0.0": {Name: "c", Version: "1.0.0"}},
			"d":    {"1.0.0": {Name: "d", Version: "1.0.0"}},
		},
	}

	opts := Options{Workers: 2}.WithDefaults()
	source := &pubgrubSource{
		ctx:            context.Background(),
		fetcher:        fetcher,
		lister:         fetcher,
		parser:         mockParser{},
		opts:           opts,
		cache:          make(map[string]*Package),
		hintedVersions: make(map[string]map[string]bool),
	}

	solution := pubgrub.Solution{
		pubgrub.NameVersion{Name: pubgrub.MakeName("$$root"), Version: pubgrub.SimpleVersion("0.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("root"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("a"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("b"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("c"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("d"), Version: pubgrub.SimpleVersion("1.0.0")},
	}

	resolver := &PubGrubResolver{}
	g, err := resolver.solutionToDAG(context.Background(), solution, "root", source, opts)
	if err != nil {
		t.Fatalf("solutionToDAG() unexpected error: %v", err)
	}
	if g.NodeCount() == 0 {
		t.Fatal("solutionToDAG() produced empty graph")
	}
	if got := fetcher.maxSeen.Load(); got > int32(opts.Workers) {
		t.Fatalf("max concurrent FetchVersion = %d, want <= %d", got, opts.Workers)
	}
	if got := fetcher.maxSeen.Load(); got < 2 {
		t.Fatalf("expected parallel package fetch (>1 concurrent), got %d", got)
	}

	for _, name := range []string{"a", "b", "c", "d"} {
		if _, ok := g.Node(name); !ok {
			t.Fatalf("expected node %q to exist", name)
		}
	}
}

func TestPubGrubResolver_solutionToDAG_Cancellation(t *testing.T) {
	fetcher := &countingVersionLister{
		delay: 100 * time.Millisecond,
		packages: map[string]map[string]*Package{
			"root": {"1.0.0": {Name: "root", Version: "1.0.0"}},
			"a":    {"1.0.0": {Name: "a", Version: "1.0.0"}},
			"b":    {"1.0.0": {Name: "b", Version: "1.0.0"}},
		},
	}
	opts := Options{Workers: 2}.WithDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := &pubgrubSource{
		ctx:            ctx,
		fetcher:        fetcher,
		lister:         fetcher,
		parser:         mockParser{},
		opts:           opts,
		cache:          make(map[string]*Package),
		hintedVersions: make(map[string]map[string]bool),
	}
	solution := pubgrub.Solution{
		pubgrub.NameVersion{Name: pubgrub.MakeName("$$root"), Version: pubgrub.SimpleVersion("0.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("root"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("a"), Version: pubgrub.SimpleVersion("1.0.0")},
		pubgrub.NameVersion{Name: pubgrub.MakeName("b"), Version: pubgrub.SimpleVersion("1.0.0")},
	}
	resolver := &PubGrubResolver{}

	g, err := resolver.solutionToDAG(ctx, solution, "root", source, opts)
	if err != nil {
		t.Fatalf("solutionToDAG() should not fail on canceled package fetches, got: %v", err)
	}
	if g == nil {
		t.Fatalf("solutionToDAG() returned nil graph")
	}
	// On cancellation, package fetches may fail; root node from solution should still exist.
	if _, ok := g.Node("root"); !ok {
		t.Fatalf("expected root node to exist")
	}
	if got := fetcher.maxSeen.Load(); got > int32(opts.Workers) {
		t.Fatalf("max concurrent FetchVersion = %d, want <= %d", got, opts.Workers)
	}
}

// TestPubGrubSource_HintsVersionsFromEqualConstraints verifies that versions
// specified with "=" prefix constraints (used by Go modules) are hinted for
// inclusion in GetVersions. This is critical for Go pseudo-versions like
// "v0.0.0-20231006140011-7918f672742d" that aren't listed by the Go proxy's
// @v/list endpoint but appear in go.mod files as dependencies.
func TestPubGrubSource_HintsVersionsFromEqualConstraints(t *testing.T) {
	// This fetcher simulates a registry where ListVersions returns no versions
	// (like Go modules with only pseudo-versions), forcing reliance on hints.
	fetcher := &mockVersionLister{
		packages: map[string]map[string]*Package{
			"root": {
				"1.0.0": {
					Name:    "root",
					Version: "1.0.0",
					Dependencies: []Dependency{
						// Go-style constraint with = prefix
						{Name: "pseudo-dep", Constraint: "=v0.0.0-20231006140011-abc123"},
					},
				},
			},
			"pseudo-dep": {
				// The registry knows about this version but doesn't list it
				"v0.0.0-20231006140011-abc123": {
					Name:    "pseudo-dep",
					Version: "v0.0.0-20231006140011-abc123",
				},
			},
		},
	}

	// Override ListVersions to return empty (simulating Go proxy @v/list)
	emptyLister := &emptyVersionLister{fetcher: fetcher}

	resolver, err := NewPubGrubResolver("test", emptyLister, mockParser{})
	if err != nil {
		t.Fatalf("NewPubGrubResolver failed: %v", err)
	}

	ctx := context.Background()
	dag, err := resolver.Resolve(ctx, "root", Options{Version: "1.0.0"})
	if err != nil {
		t.Fatalf("Resolve failed: %v (should succeed with hinted version)", err)
	}

	// Verify the pseudo-version dependency was resolved via hinting
	if _, ok := dag.Node("pseudo-dep"); !ok {
		t.Error("expected pseudo-dep to be resolved via version hinting")
	}
}

// emptyVersionLister wraps mockVersionLister but returns empty from ListVersions
// to simulate Go proxy behavior for modules without tagged releases.
type emptyVersionLister struct {
	fetcher *mockVersionLister
}

func (e *emptyVersionLister) Fetch(ctx context.Context, name string, refresh bool) (*Package, error) {
	return e.fetcher.Fetch(ctx, name, refresh)
}

func (e *emptyVersionLister) FetchVersion(ctx context.Context, name, version string, refresh bool) (*Package, error) {
	return e.fetcher.FetchVersion(ctx, name, version, refresh)
}

func (e *emptyVersionLister) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	// Simulate Go proxy returning no versions for pseudo-version-only modules
	return []string{}, nil
}
