package deps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

type mockManifestParserForDetect struct {
	typeName     string
	supportsFunc func(string) bool
}

func (m *mockManifestParserForDetect) Type() string { return m.typeName }
func (m *mockManifestParserForDetect) Supports(filename string) bool {
	if m.supportsFunc != nil {
		return m.supportsFunc(filename)
	}
	return false
}
func (m *mockManifestParserForDetect) IncludesTransitive() bool { return false }
func (m *mockManifestParserForDetect) Parse(path string, opts Options) (*ManifestResult, error) {
	return &ManifestResult{}, nil
}

func TestDetectManifest(t *testing.T) {
	poetry := &mockManifestParserForDetect{
		typeName: "poetry",
		supportsFunc: func(f string) bool {
			return f == "pyproject.toml"
		},
	}
	requirements := &mockManifestParserForDetect{
		typeName: "requirements",
		supportsFunc: func(f string) bool {
			return f == "requirements.txt"
		},
	}

	tests := []struct {
		name     string
		path     string
		parsers  []ManifestParser
		wantType string
		wantErr  bool
	}{
		{
			name:     "matches poetry",
			path:     "/some/path/pyproject.toml",
			parsers:  []ManifestParser{poetry, requirements},
			wantType: "poetry",
			wantErr:  false,
		},
		{
			name:     "matches requirements",
			path:     "/project/requirements.txt",
			parsers:  []ManifestParser{poetry, requirements},
			wantType: "requirements",
			wantErr:  false,
		},
		{
			name:    "no match",
			path:    "/project/unknown.yaml",
			parsers: []ManifestParser{poetry, requirements},
			wantErr: true,
		},
		{
			name:    "no parsers",
			path:    "/project/anything.txt",
			parsers: []ManifestParser{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := DetectManifest(tt.path, tt.parsers...)
			if tt.wantErr {
				if err == nil {
					t.Error("DetectManifest() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectManifest() unexpected error: %v", err)
			}
			if parser.Type() != tt.wantType {
				t.Errorf("DetectManifest().Type() = %q, want %q", parser.Type(), tt.wantType)
			}
		})
	}
}

func TestDetectManifestFirstMatch(t *testing.T) {
	// Test that first matching parser is returned
	p1 := &mockManifestParserForDetect{
		typeName: "first",
		supportsFunc: func(f string) bool {
			return f == "test.txt"
		},
	}
	p2 := &mockManifestParserForDetect{
		typeName: "second",
		supportsFunc: func(f string) bool {
			return f == "test.txt"
		},
	}

	parser, err := DetectManifest("/path/test.txt", p1, p2)
	if err != nil {
		t.Fatalf("DetectManifest() error: %v", err)
	}
	if parser.Type() != "first" {
		t.Errorf("DetectManifest() should return first matching parser, got %q", parser.Type())
	}
}

type resolveBehavior struct {
	err   error
	delay time.Duration
}

type trackingResolver struct {
	behavior map[string]resolveBehavior
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func (r *trackingResolver) Name() string { return "tracking-resolver" }

func (r *trackingResolver) Resolve(ctx context.Context, pkg string, opts Options) (*dag.DAG, error) {
	cur := r.inFlight.Add(1)
	for {
		prev := r.maxSeen.Load()
		if cur <= prev || r.maxSeen.CompareAndSwap(prev, cur) {
			break
		}
	}
	defer r.inFlight.Add(-1)

	b := r.behavior[pkg]
	if b.delay > 0 {
		select {
		case <-time.After(b.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if b.err != nil {
		return nil, b.err
	}

	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: pkg})
	_ = g.AddNode(dag.Node{ID: pkg + "-child"})
	_ = g.AddEdge(dag.Edge{From: pkg, To: pkg + "-child"})
	return g, nil
}

func edgePairs(g *dag.DAG) []string {
	pairs := make([]string, 0, g.EdgeCount())
	for _, e := range g.Edges() {
		pairs = append(pairs, fmt.Sprintf("%s->%s", e.From, e.To))
	}
	return pairs
}

func TestResolveAndMerge_WorkerBoundAndPartialFailure(t *testing.T) {
	depsList := []Dependency{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
		{Name: "d"},
	}
	resolver := &trackingResolver{
		behavior: map[string]resolveBehavior{
			"a": {delay: 30 * time.Millisecond},
			"b": {delay: 30 * time.Millisecond},
			"c": {delay: 30 * time.Millisecond, err: errors.New("boom")},
			"d": {delay: 30 * time.Millisecond},
		},
	}

	g, err := ResolveAndMerge(context.Background(), resolver, depsList, Options{Workers: 2})
	if err != nil {
		t.Fatalf("ResolveAndMerge() unexpected error: %v", err)
	}
	if got := resolver.maxSeen.Load(); got > 2 {
		t.Fatalf("max concurrent resolves = %d, want <= 2", got)
	}

	// Failed dependency should still be attached as a leaf from project root.
	if _, ok := g.Node("c"); !ok {
		t.Fatalf("expected failed dependency node %q to exist", "c")
	}
	rootChildren := g.Children(ProjectRootNodeID)
	if !slices.Contains(rootChildren, "c") {
		t.Fatalf("expected root edge to failed dependency %q", "c")
	}
}

func TestResolveAndMerge_DeterministicMergeOrder(t *testing.T) {
	depsList := []Dependency{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
		{Name: "d"},
	}
	resolver := &trackingResolver{
		behavior: map[string]resolveBehavior{
			"a": {delay: 8 * time.Millisecond},
			"b": {delay: 1 * time.Millisecond},
			"c": {delay: 5 * time.Millisecond},
			"d": {delay: 3 * time.Millisecond},
		},
	}

	var baseline []string
	for i := range 8 {
		g, err := ResolveAndMerge(context.Background(), resolver, depsList, Options{Workers: 4})
		if err != nil {
			t.Fatalf("ResolveAndMerge() run %d failed: %v", i, err)
		}
		got := edgePairs(g)
		if i == 0 {
			baseline = got
			continue
		}
		if !slices.Equal(baseline, got) {
			t.Fatalf("non-deterministic edge order on run %d:\nbase=%v\ngot=%v", i, baseline, got)
		}
	}
}

func TestResolveAndMerge_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := &trackingResolver{
		behavior: map[string]resolveBehavior{
			"a": {delay: 50 * time.Millisecond},
			"b": {delay: 50 * time.Millisecond},
		},
	}

	_, err := ResolveAndMerge(ctx, resolver, []Dependency{{Name: "a"}, {Name: "b"}}, Options{Workers: 2})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveAndMerge() err = %v, want context.Canceled", err)
	}
}

type optionsCapturingResolver struct {
	mu   sync.Mutex
	opts map[string]Options
}

func (r *optionsCapturingResolver) Name() string { return "capturing-resolver" }

func (r *optionsCapturingResolver) Resolve(ctx context.Context, pkg string, opts Options) (*dag.DAG, error) {
	r.mu.Lock()
	if r.opts == nil {
		r.opts = make(map[string]Options)
	}
	r.opts[pkg] = opts
	r.mu.Unlock()

	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: pkg})
	return g, nil
}

func TestResolveAndMerge_PassesPinnedAndConstraintOptions(t *testing.T) {
	resolver := &optionsCapturingResolver{}
	depsList := []Dependency{
		{Name: "cargo-style", Constraint: "1.0"},
		{Name: "npm-style", Constraint: "^4.18.0"},
		{Name: "locked", Pinned: "2.1.3"},
	}

	_, err := ResolveAndMerge(context.Background(), resolver, depsList, Options{Workers: 2})
	if err != nil {
		t.Fatalf("ResolveAndMerge() unexpected error: %v", err)
	}

	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	if got := resolver.opts["cargo-style"]; got.Constraint != "1.0" || got.Version != "" {
		t.Fatalf("cargo-style opts = %+v, want Constraint=1.0 and empty Version", got)
	}
	if got := resolver.opts["npm-style"]; got.Constraint != "^4.18.0" || got.Version != "" {
		t.Fatalf("npm-style opts = %+v, want Constraint=^4.18.0 and empty Version", got)
	}
	if got := resolver.opts["locked"]; got.Version != "2.1.3" || got.Constraint != "" {
		t.Fatalf("locked opts = %+v, want Version=2.1.3 and empty Constraint", got)
	}
}
