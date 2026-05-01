package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/core/deps/metadata"
	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/integrations/github"
)

// Default timeout for GitHub operations.
const defaultGitHubTimeout = 5 * time.Minute

// parseGitHubCommand creates the github subcommand for parsing from GitHub repos.
func (c *CLI) parseGitHubCommand(flags *parseFlags) *cobra.Command {
	var (
		publicOnly bool
		timeout    time.Duration
		ref        string
	)

	cmd := &cobra.Command{
		Use:   "github [owner/repo]",
		Short: "Parse dependencies from a GitHub repository",
		Long: `Interactive workflow to parse dependencies from a GitHub repository.

If not logged in, prompts you to authenticate with GitHub first.
If no repository is specified, shows an interactive list to select one.
Then lets you select a branch or tag, and a manifest file from the repository.

The --ref flag specifies a branch, tag, or commit SHA directly.
Without it, an interactive picker lets you choose from available refs
(defaults to the repository's default branch).

Examples:
  stacktower parse github                              # Full interactive flow
  stacktower parse github owner/repo                   # Select ref + manifest
  stacktower parse github owner/repo --ref v2.0.0      # Parse at a specific tag
  stacktower parse github owner/repo --ref main        # Explicit branch`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runParseGitHub(cmd.Context(), args, flags, publicOnly, timeout, ref)
		},
	}

	cmd.Flags().BoolVar(&publicOnly, "public-only", false, "show only public repositories")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultGitHubTimeout, "timeout for GitHub operations")
	cmd.Flags().StringVar(&ref, "ref", "", "git ref (branch, tag, or commit SHA)")

	return cmd
}

// repoSelection is the output of resolveRepo: the repository coordinates plus
// any manifests/default-branch that the interactive picker happened to
// surface up-front (so we can skip a second DetectManifests/GetRepoInfo call
// in the common case).
type repoSelection struct {
	Owner         string
	Repo          string
	DefaultBranch string
	Manifests     []github.ManifestFile // may be nil when resolved from argv
}

func (c *CLI) runParseGitHub(ctx context.Context, args []string, flags *parseFlags, publicOnly bool, timeout time.Duration, ref string) error {
	sess, err := loadGitHubSession(ctx)
	if err != nil {
		ui.PrintWarning("Not logged in to GitHub. Starting login flow...")
		ui.PrintNewline()
		sess, err = c.runGitHubLogin(ctx)
		if err != nil {
			return WrapSystemError(err, "GitHub login failed",
				"Run `stacktower github login` to authenticate and try again.")
		}
	}

	c.Logger.Debug("Authenticated as", "user", sess.User.Login)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := github.NewContentClient(sess.AccessToken)

	sel, cancelled, err := c.resolveRepo(ctx, client, args, publicOnly)
	if err != nil {
		return err
	}
	if cancelled {
		return ErrCancelled
	}

	selectedRef, defaultBranch, cancelled, err := c.resolveRef(ctx, client, sel.Owner, sel.Repo, sel.DefaultBranch, ref)
	if err != nil {
		return err
	}
	if cancelled {
		return ErrCancelled
	}

	refLabel := selectedRef
	if selectedRef == defaultBranch {
		refLabel = selectedRef + " (default)"
	}
	ui.PrintInfo("Ref: %s", ui.StyleHighlight.Render(refLabel))

	manifest, cancelled, err := c.resolveManifest(ctx, client, sel.Owner, sel.Repo, selectedRef, sel.Manifests)
	if err != nil {
		return err
	}
	if cancelled {
		return ErrCancelled
	}

	return c.fetchAndParseManifest(ctx, client, flags, sel.Owner, sel.Repo, selectedRef, manifest)
}

// resolveRepo picks the (owner, repo) coordinates the user wants to parse.
// When args[0] is set, the repo is taken verbatim. Otherwise we scan the
// user's accessible repositories for manifests and show an interactive
// picker, which also lets us reuse the manifest list / default branch that
// scan returned.
//
// Returns cancelled=true when the user dismissed the picker without selecting.
func (c *CLI) resolveRepo(ctx context.Context, client *github.ContentClient, args []string, publicOnly bool) (repoSelection, bool, error) {
	if len(args) == 1 {
		owner, repo, err := github.ParseRepoRef(args[0])
		if err != nil {
			return repoSelection{}, false, WrapUserError(err, "invalid GitHub repository reference",
				"Use the form owner/repo (e.g. stacktower-io/stacktower).")
		}
		ui.PrintInfo("Repository: %s", ui.StyleHighlight.Render(owner+"/"+repo))
		return repoSelection{Owner: owner, Repo: repo}, false, nil
	}

	spinner := ui.NewSpinnerWithContext(ctx, "Fetching and scanning repositories...")
	spinner.Start()
	manifestPatterns := deps.SupportedManifests(languages.All)
	rwm, err := client.ScanReposForManifests(ctx, manifestPatterns, publicOnly)
	spinner.Stop()
	if err != nil {
		return repoSelection{}, false, WrapSystemError(err, "failed to scan repositories",
			"Check your network connection and GitHub token scopes.")
	}

	if len(rwm) == 0 {
		return repoSelection{}, false, NewUserError(
			"no repositories with manifest files found",
			"Make sure your GitHub App installation has access to at least one repo with a supported manifest.",
		)
	}

	ui.PrintSuccess("Found %d repositories with manifests", len(rwm))
	ui.PrintNewline()

	m := ui.NewRepoListModel(rwm)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return repoSelection{}, false, WrapSystemError(err, "repo picker failed", "")
	}

	fm, ok := finalModel.(ui.RepoListModel)
	if !ok || fm.Selected == nil {
		ui.PrintDetail("No selection made")
		return repoSelection{}, true, nil
	}

	parts := strings.SplitN(fm.Selected.Repo.Repo.FullName, "/", 2)
	if len(parts) != 2 {
		return repoSelection{}, false, NewSystemError(
			fmt.Sprintf("invalid repo name returned by GitHub: %s", fm.Selected.Repo.Repo.FullName),
			"This is an internal error. Please report this issue.",
		)
	}

	return repoSelection{
		Owner:         parts[0],
		Repo:          parts[1],
		DefaultBranch: fm.Selected.Repo.Repo.DefaultBranch,
		Manifests:     fm.Selected.Repo.Manifests,
	}, false, nil
}

// resolveRef picks the git ref to parse. If the user already passed --ref we
// use it verbatim; otherwise we fall through to the interactive picker. The
// returned defaultBranch is used for the "(default)" label in the UI.
func (c *CLI) resolveRef(ctx context.Context, client *github.ContentClient, owner, repo, defaultBranch, ref string) (string, string, bool, error) {
	if ref != "" {
		return ref, defaultBranch, false, nil
	}
	selectedRef, defaultBranch, err := c.selectRef(ctx, client, owner, repo, defaultBranch)
	if err != nil {
		return "", defaultBranch, false, err
	}
	if selectedRef == "" {
		ui.PrintDetail("No selection made")
		return "", defaultBranch, true, nil
	}
	return selectedRef, defaultBranch, false, nil
}

// resolveManifest selects which manifest file to parse. If resolveRepo's
// picker already surfaced a list we reuse it; otherwise we call
// DetectManifests against the chosen ref. Single matches are auto-selected;
// multiple matches trigger an interactive picker.
func (c *CLI) resolveManifest(ctx context.Context, client *github.ContentClient, owner, repo, ref string, manifests []github.ManifestFile) (github.ManifestFile, bool, error) {
	if len(manifests) == 0 {
		spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Scanning %s/%s@%s for manifests...", owner, repo, ref))
		spinner.Start()
		found, err := client.DetectManifests(ctx, owner, repo, ref, deps.SupportedManifests(languages.All))
		spinner.Stop()
		if err != nil {
			return github.ManifestFile{}, false, WrapSystemError(err, "failed to detect manifests",
				"Check that the repository and ref exist and are accessible with your GitHub token.")
		}
		manifests = found
	}

	if len(manifests) == 0 {
		return github.ManifestFile{}, false, NewUserError(
			fmt.Sprintf("no manifest files found in %s/%s@%s", owner, repo, ref),
			"Verify the ref contains a supported manifest (package.json, poetry.lock, Cargo.lock, ...)",
		)
	}

	if len(manifests) == 1 {
		m := manifests[0]
		ui.PrintInfo("Found: %s (%s)", ui.StyleHighlight.Render(m.Name), m.Language)
		return m, false, nil
	}

	ui.PrintInfo("Found %d manifest files", len(manifests))
	ui.PrintNewline()
	mm := ui.NewManifestListModel(manifests)
	mp := tea.NewProgram(mm)
	mfinalModel, err := mp.Run()
	if err != nil {
		return github.ManifestFile{}, false, WrapSystemError(err, "manifest picker failed", "")
	}

	mfm, ok := mfinalModel.(ui.ManifestListModel)
	if !ok || mfm.Selected == nil {
		ui.PrintDetail("No manifest selected")
		return github.ManifestFile{}, true, nil
	}
	return *mfm.Selected, false, nil
}

// fetchAndParseManifest pulls the manifest bytes, runs the parse pipeline,
// annotates the root node with GitHub metadata, and hands off to finishParse.
func (c *CLI) fetchAndParseManifest(ctx context.Context, client *github.ContentClient, flags *parseFlags, owner, repo, ref string, manifest github.ManifestFile) error {
	fetchSpinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Fetching %s@%s...", manifest.Path, ref))
	fetchSpinner.Start()
	content, err := client.FetchFileRaw(ctx, owner, repo, manifest.Path, ref)
	if err != nil {
		fetchSpinner.StopWithError("Failed to fetch manifest")
		return WrapSystemError(err, "failed to fetch manifest from GitHub",
			"Check that the ref exists and your token has access to the repository.")
	}
	fetchSpinner.Stop()

	lang := languages.Find(manifest.Language)
	if lang == nil {
		return NewSystemError(
			fmt.Sprintf("unsupported language returned by manifest detector: %s", manifest.Language),
			"This is an internal error. Please report this issue.",
		)
	}

	manifestName := filepath.Base(manifest.Name)

	name := flags.name
	if name == "" {
		name = repo
	}
	// Match the behavior of `parse <lang>`: only write to a file when the user
	// explicitly passed -o (or when stdout is not a TTY, handled by
	// finishParse). A suggestion is surfaced as a "next step" hint instead of
	// silently fabricating an implicit output path.
	output := flags.output

	ui.PrintNewline()

	start := time.Now()
	opts := flags.Options
	opts.Language = lang.Name
	opts.Manifest = content
	opts.ManifestFilename = manifestName
	opts.ManifestPath = ""
	opts.SkipEnrich = !flags.enrich

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, flags.scan,
		fmt.Sprintf("Parsing %s...", manifestName), flags.MaxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("parse %s", manifestName), err)
	}
	if name != "" {
		if err := result.Graph.RenameNode(graph.ProjectRootNodeID, name); err != nil {
			c.Logger.Debug("rename root node failed", "from", graph.ProjectRootNodeID, "to", name, "err", err)
		}
	}

	if info, infoErr := client.GetRepoInfo(ctx, owner, repo); infoErr == nil {
		annotateGitHubRootNode(result.Graph, name, owner, repo, info)
	} else {
		c.Logger.Debug("github root metadata fetch failed", "owner", owner, "repo", repo, "error", infoErr)
	}

	return finishParse(finishParseOpts{
		Graph:          result.Graph,
		Output:         output,
		LangName:       lang.Name,
		Source:         manifestName,
		CacheHit:       result.CacheHit,
		Elapsed:        time.Since(start),
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
		Ref:            ref,
	})
}

func annotateGitHubRootNode(g *dag.DAG, rootID, owner, repo string, info *github.RepoInfo) {
	if g == nil || info == nil {
		return
	}
	id := rootID
	if id == "" {
		id = graph.ProjectRootNodeID
	}
	node, ok := g.Node(id)
	if !ok {
		node, ok = g.Node(graph.ProjectRootNodeID)
		if !ok {
			return
		}
	}
	if node.Meta == nil {
		node.Meta = dag.Metadata{}
	}
	node.Meta[metadata.RepoURL] = fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	node.Meta[metadata.RepoOwner] = owner
	node.Meta[metadata.RepoArchived] = info.Archived
	if info.Description != "" {
		node.Meta[metadata.RepoDescription] = info.Description
		if _, ok := node.Meta["description"]; !ok {
			node.Meta["description"] = info.Description
		}
	}
	if info.Stars > 0 {
		node.Meta[metadata.RepoStars] = info.Stars
	}
	if info.Language != "" {
		node.Meta[metadata.RepoLanguage] = info.Language
	}
	if info.License != "" {
		node.Meta[metadata.RepoLicense] = info.License
		if _, ok := node.Meta["license"]; !ok {
			node.Meta["license"] = info.License
		}
	}
	if len(info.Topics) > 0 {
		node.Meta[metadata.RepoTopics] = info.Topics
	}
}

// selectRef fetches repo info, branches, and tags, then lets the user pick a ref.
// If defaultBranch is already known (from the interactive repo list), we skip GetRepoInfo.
// Returns (selectedRef, defaultBranch, error). selectedRef is empty if the user cancelled.
func (c *CLI) selectRef(ctx context.Context, client *github.ContentClient, owner, repo, defaultBranch string) (string, string, error) {
	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Fetching refs for %s/%s...", owner, repo))
	spinner.Start()

	type branchResult struct {
		branches []github.Branch
		err      error
	}
	type tagResult struct {
		tags []github.Tag
		err  error
	}
	type infoResult struct {
		info *github.RepoInfo
		err  error
	}

	// Channels are buffered with capacity 1 so each goroutine can always
	// enqueue its result without blocking, even if the parent function
	// returns early (e.g. ctx cancellation or a read error above). This
	// prevents goroutine leaks: the sender never parks on a send, and the
	// HTTP client already honours ctx to bound the lifetime of each call.
	branchCh := make(chan branchResult, 1)
	tagCh := make(chan tagResult, 1)
	infoCh := make(chan infoResult, 1)

	go func() {
		b, err := client.ListBranches(ctx, owner, repo)
		branchCh <- branchResult{b, err}
	}()
	go func() {
		t, err := client.ListTags(ctx, owner, repo)
		tagCh <- tagResult{t, err}
	}()

	needInfo := defaultBranch == ""
	if needInfo {
		go func() {
			info, err := client.GetRepoInfo(ctx, owner, repo)
			infoCh <- infoResult{info, err}
		}()
	}

	br := <-branchCh
	tr := <-tagCh

	if needInfo {
		ir := <-infoCh
		if ir.err == nil && ir.info != nil {
			defaultBranch = ir.info.DefaultBranch
		}
	}

	spinner.Stop()

	if br.err != nil {
		return "", defaultBranch, WrapSystemError(br.err, "failed to list branches",
			"Check that your GitHub token has access to the repository.")
	}

	// Fallback: if we still don't know the default branch, guess from available branches
	if defaultBranch == "" {
		for _, b := range br.branches {
			if b.Name == "main" || b.Name == "master" {
				defaultBranch = b.Name
				break
			}
		}
		if defaultBranch == "" && len(br.branches) > 0 {
			defaultBranch = br.branches[0].Name
		}
	}

	var tags []github.Tag
	if tr.err == nil {
		tags = tr.tags
	} else {
		ui.PrintWarning("Could not load tags: %v", tr.err)
	}

	// Fast path: if only the default branch exists and no tags, skip the picker
	if len(br.branches) <= 1 && len(tags) == 0 {
		return defaultBranch, defaultBranch, nil
	}

	ui.PrintSuccess("Found %d branches, %d tags", len(br.branches), len(tags))
	ui.PrintNewline()

	m := ui.NewRefListModel(br.branches, tags, defaultBranch)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", defaultBranch, WrapSystemError(err, "ref picker failed", "")
	}

	fm, ok := finalModel.(*ui.RefListModel)
	if !ok || fm.Selected == nil {
		return "", defaultBranch, nil
	}

	return fm.Selected.Name, defaultBranch, nil
}
