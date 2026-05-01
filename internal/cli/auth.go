package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/integrations/github"
)

// githubCommand creates the github command with subcommands.
func (c *CLI) githubCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub integration commands",
		Long: `Authenticate with GitHub and interact with your repositories.

Use the device flow to authenticate without needing a web browser callback.
Your session is stored in ~/.config/stacktower/sessions/`,
	}

	cmd.AddCommand(c.githubLoginCommand())
	cmd.AddCommand(c.githubLogoutCommand())
	cmd.AddCommand(c.githubWhoamiCommand())
	cmd.AddCommand(c.githubInstallCommand())
	cmd.AddCommand(c.githubUninstallCommand())

	return cmd
}

// githubLoginCommand creates the login subcommand.
func (c *CLI) githubLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with GitHub using device flow",
		Long: `Start the GitHub device authorization flow.

You'll be given a code to enter at https://github.com/login/device.
Once authorized, your session will be saved locally for future commands.

Repository access is configured when you install the GitHub App.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			existing, err := loadGitHubSession(ctx)
			if err == nil && existing != nil {
				ui.PrintInfo("Already logged in as @%s", existing.User.Login)
				ui.PrintDetail("Run 'stacktower github logout' first to re-authenticate")
				return nil
			}
			if err != nil && !isNotLoggedInError(err) {
				return err
			}

			_, err = c.runGitHubLogin(ctx)
			return err
		},
	}

	return cmd
}

// githubLogoutCommand creates the logout subcommand.
func (c *CLI) githubLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored GitHub credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deleteGitHubSession(cmd.Context()); err != nil {
				return WrapSystemError(err, "failed to delete session", "Check file permissions for ~/.config/stacktower/sessions/")
			}
			ui.PrintSuccess("Logged out")
			return nil
		},
	}
}

// githubWhoamiCommand creates the whoami subcommand.
func (c *CLI) githubWhoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently authenticated GitHub user",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sess, err := loadGitHubSession(ctx)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			spinner := ui.NewSpinnerWithContext(ctx, "Verifying session...")
			spinner.Start()

			client := github.NewContentClient(sess.AccessToken)
			user, err := client.FetchUser(ctx)
			if err != nil {
				spinner.StopWithError("Session invalid")
				return WrapSystemError(err, "failed to verify GitHub session", "Your session may have expired. Try 'stacktower github logout' and re-login.")
			}
			spinner.Stop()

			ui.PrintHeader("GitHub Session")
			ui.PrintKeyValue("Username", "@"+user.Login)
			if user.Name != "" {
				ui.PrintKeyValue("Name", user.Name)
			}
			if user.Email != "" {
				ui.PrintKeyValue("Email", user.Email)
			}
			ui.PrintKeyValue("Logged in", sess.CreatedAt.Format("Jan 2, 2006"))
			ui.PrintKeyValue("Expires", sess.ExpiresAt.Format("Jan 2, 2006"))

			if remaining := time.Until(sess.ExpiresAt); remaining > 0 && remaining < sessionExpiryWarning {
				days := int(remaining.Hours() / 24)
				if days < 1 {
					ui.PrintWarning("Session expires in less than a day — run `stacktower github login` to refresh.")
				} else {
					ui.PrintWarning("Session expires in %d day(s) — run `stacktower github login` to refresh.", days)
				}
			}

			installation, err := client.HasAppInstallation(ctx, buildinfo.GitHubAppSlug)
			if err != nil {
				ui.PrintNewline()
				ui.PrintWarning("Could not check app installation status: %v", err)
			} else {
				ui.PrintNewline()
				if installation != nil {
					ui.PrintKeyValue("App Status", ui.StyleSuccess.Render("Installed")+" (@"+installation.Account.Login+")")
				} else {
					ui.PrintKeyValue("App Status", ui.StyleWarning.Render("Not installed"))
					ui.PrintDetail("Run 'stacktower github install' to install the app")
				}
			}

			return nil
		},
	}
}

// githubInstallCommand creates the install subcommand.
func (c *CLI) githubInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install or manage the Stacktower GitHub App",
		Long: `Open the GitHub App installation page in your browser.

This allows you to install the Stacktower app on your account or organization,
and configure which repositories it can access.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			sess, err := loadGitHubSession(ctx)
			if err != nil {
				return err
			}

			client := github.NewContentClient(sess.AccessToken)
			installation, err := client.HasAppInstallation(ctx, buildinfo.GitHubAppSlug)
			if err != nil {
				c.Logger.Debug("failed to check app installation", "error", err)
			}

			installURL := fmt.Sprintf("https://github.com/apps/%s/installations/new", buildinfo.GitHubAppSlug)

			if installation != nil {
				ui.PrintInfo("App already installed for @%s", installation.Account.Login)
				ui.PrintDetail("Opening settings to manage installation...")
				installURL = "https://github.com/settings/installations"
			} else {
				ui.PrintInfo("Opening GitHub App installation page...")
			}

			ui.PrintKeyValue("URL", ui.StyleLink.Render(installURL))

			if err := openBrowser(installURL); err != nil {
				ui.PrintDetail("Copy the URL above and paste it in your browser")
			}

			return nil
		},
	}
}

// githubUninstallCommand creates the uninstall subcommand.
func (c *CLI) githubUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the Stacktower GitHub App",
		Long: `Open the GitHub App settings page to uninstall Stacktower.

This removes Stacktower's access to your repositories. You can re-install
at any time with 'stacktower github install'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			sess, err := loadGitHubSession(ctx)
			if err != nil {
				return err
			}

			client := github.NewContentClient(sess.AccessToken)
			installation, err := client.HasAppInstallation(ctx, buildinfo.GitHubAppSlug)
			if err != nil {
				c.Logger.Debug("failed to check app installation", "error", err)
			}

			if installation == nil {
				ui.PrintInfo("GitHub App is not installed")
				ui.PrintDetail("Run 'stacktower github install' to install it")
				return nil
			}

			uninstallURL := fmt.Sprintf("https://github.com/settings/installations/%d", installation.ID)

			ui.PrintNewline()
			ui.PrintHeader("Uninstall Stacktower GitHub App")
			ui.PrintKeyValue("Installed on", "@"+installation.Account.Login)
			ui.PrintKeyValue("URL", ui.StyleLink.Render(uninstallURL))
			ui.PrintNewline()
			ui.PrintDetail("The settings page will open in your browser.")
			ui.PrintDetail("Scroll to \"Danger zone\" and click Uninstall.")
			ui.PrintNewline()

			if err := openBrowser(uninstallURL); err != nil {
				ui.PrintDetail("Copy the URL above and paste it in your browser")
			}

			return nil
		},
	}
}
