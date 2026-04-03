package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/integrations/github"
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

func (c *CLI) runParseGitHub(ctx context.Context, args []string, flags *parseFlags, publicOnly bool, timeout time.Duration, ref string) error {
	sess, err := loadGitHubSession(ctx)
	if err != nil {
		ui.PrintWarning("Not logged in to GitHub. Starting login flow...")
		ui.PrintNewline()
		sess, err = c.runGitHubLogin(ctx)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
	}

	c.Logger.Debug("Authenticated as", "user", sess.User.Login)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := github.NewContentClient(sess.AccessToken)

	var owner, repo, defaultBranch string
	var manifests []github.ManifestFile
	var selectedManifest github.ManifestFile

	if len(args) == 1 {
		var err error
		owner, repo, err = github.ParseRepoRef(args[0])
		if err != nil {
			return err
		}
		ui.PrintInfo("Repository: %s", ui.StyleHighlight.Render(owner+"/"+repo))
	} else {
		spinner := ui.NewSpinnerWithContext(ctx, "Fetching and scanning repositories...")
		spinner.Start()
		manifestPatterns := deps.SupportedManifests(languages.All)
		rwm, err := client.ScanReposForManifests(ctx, manifestPatterns, publicOnly)
		spinner.Stop()
		if err != nil {
			return fmt.Errorf("scan repos: %w", err)
		}

		if len(rwm) == 0 {
			return fmt.Errorf("no repositories with manifest files found")
		}

		ui.PrintSuccess("Found %d repositories with manifests", len(rwm))
		ui.PrintNewline()

		m := ui.NewRepoListModel(rwm)
		p := tea.NewProgram(m)
		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		fm, ok := finalModel.(ui.RepoListModel)
		if !ok || fm.Selected == nil {
			ui.PrintDetail("No selection made")
			return nil
		}

		parts := strings.SplitN(fm.Selected.Repo.Repo.FullName, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid repo name: %s", fm.Selected.Repo.Repo.FullName)
		}
		owner, repo = parts[0], parts[1]
		defaultBranch = fm.Selected.Repo.Repo.DefaultBranch
		manifests = fm.Selected.Repo.Manifests
	}

	// -------------------------------------------------------------------------
	// Ref selection: determine which branch/tag/commit to use
	// -------------------------------------------------------------------------

	selectedRef := ref

	if selectedRef == "" {
		var err error
		selectedRef, defaultBranch, err = c.selectRef(ctx, client, owner, repo, defaultBranch)
		if err != nil {
			return err
		}
		if selectedRef == "" {
			ui.PrintDetail("No selection made")
			return nil
		}
	}

	refLabel := selectedRef
	if selectedRef == defaultBranch {
		refLabel = selectedRef + " (default)"
	}
	ui.PrintInfo("Ref: %s", ui.StyleHighlight.Render(refLabel))

	// -------------------------------------------------------------------------
	// Manifest detection and selection
	// -------------------------------------------------------------------------

	if selectedManifest.Name == "" {
		if len(manifests) == 0 {
			spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Scanning %s/%s@%s for manifests...", owner, repo, selectedRef))
			spinner.Start()
			manifests, err = client.DetectManifests(ctx, owner, repo, selectedRef, deps.SupportedManifests(languages.All))
			spinner.Stop()
			if err != nil {
				return fmt.Errorf("detect manifests: %w", err)
			}
		}

		if len(manifests) == 0 {
			return fmt.Errorf("no manifest files found in %s/%s@%s", owner, repo, selectedRef)
		}

		if len(manifests) == 1 {
			selectedManifest = manifests[0]
			ui.PrintInfo("Found: %s (%s)", ui.StyleHighlight.Render(selectedManifest.Name), selectedManifest.Language)
		} else {
			ui.PrintInfo("Found %d manifest files", len(manifests))
			ui.PrintNewline()
			mm := ui.NewManifestListModel(manifests)
			mp := tea.NewProgram(mm)
			mfinalModel, err := mp.Run()
			if err != nil {
				return err
			}

			mfm, ok := mfinalModel.(ui.ManifestListModel)
			if !ok || mfm.Selected == nil {
				ui.PrintDetail("No manifest selected")
				return nil
			}
			selectedManifest = *mfm.Selected
		}
	}

	fetchSpinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Fetching %s@%s...", selectedManifest.Path, selectedRef))
	fetchSpinner.Start()
	content, err := client.FetchFileRaw(ctx, owner, repo, selectedManifest.Path, selectedRef)
	if err != nil {
		fetchSpinner.StopWithError("Failed to fetch manifest")
		return fmt.Errorf("fetch manifest: %w", err)
	}
	fetchSpinner.Stop()

	tmpDir, err := os.MkdirTemp("", "stacktower-github-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, selectedManifest.Name)
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	lang := languages.Find(selectedManifest.Language)
	if lang == nil {
		return fmt.Errorf("unsupported language: %s", selectedManifest.Language)
	}

	if flags.name == "" {
		flags.name = repo
	}
	if flags.output == "" {
		flags.output = repo + ".json"
	}

	ui.PrintNewline()

	start := time.Now()
	opts := flags.Options
	opts.Language = lang.Name
	opts.Manifest = content
	opts.ManifestFilename = filepath.Base(tmpFile)
	opts.ManifestPath = tmpFile
	opts.SkipEnrich = !flags.enrich

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, flags.scan,
		fmt.Sprintf("Parsing %s...", filepath.Base(tmpFile)), flags.MaxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("parse %s", filepath.Base(tmpFile)), err)
	}

	name := flags.name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(tmpFile), filepath.Ext(tmpFile))
	}
	if name != "" {
		result.Graph.RenameNode(graph.ProjectRootNodeID, name) //nolint:errcheck // non-critical rename
	}

	if info, infoErr := client.GetRepoInfo(ctx, owner, repo); infoErr == nil {
		annotateGitHubRootNode(result.Graph, name, owner, repo, info)
	} else {
		c.Logger.Debug("github root metadata fetch failed", "owner", owner, "repo", repo, "error", infoErr)
	}

	return finishParse(finishParseOpts{
		Graph:          result.Graph,
		Output:         flags.output,
		LangName:       lang.Name,
		Source:         filepath.Base(tmpFile),
		CacheHit:       result.CacheHit,
		Elapsed:        time.Since(start),
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
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
		return "", defaultBranch, fmt.Errorf("list branches: %w", br.err)
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
		return "", defaultBranch, err
	}

	fm, ok := finalModel.(ui.RefListModel)
	if !ok || fm.Selected == nil {
		return "", defaultBranch, nil
	}

	return fm.Selected.Name, defaultBranch, nil
}
