package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/integrations/github"
	"github.com/stacktower-io/stacktower/pkg/session"
)

func (c *CLI) runGitHubLogin(ctx context.Context) (*session.Session, error) {
	if buildinfo.GitHubAppClientID == "" {
		return nil, NewSystemError("GitHub login not available in this build", "This binary was built without a GitHub App client ID.")
	}

	oauthClient := github.NewOAuthClient(github.OAuthConfig{ClientID: buildinfo.GitHubAppClientID})

	loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	deviceResp, err := oauthClient.RequestDeviceCode(loginCtx)
	if err != nil {
		return nil, WrapSystemError(err, "failed to request device code", "Check your network connection and try again.")
	}

	ui.PrintNewline()
	ui.PrintHeader("GitHub Device Authorization")
	ui.PrintKeyValue("Code", ui.StyleNumber.Render(deviceResp.UserCode))
	ui.PrintKeyValue("URL", ui.StyleLink.Render(deviceResp.VerificationURI))
	ui.PrintNewline()

	if err := openBrowser(deviceResp.VerificationURI); err != nil {
		ui.PrintDetail("Copy the URL above and paste it in your browser")
	} else {
		ui.PrintDetail("Opening browser...")
	}

	spinner := ui.NewSpinnerWithContext(loginCtx, "Waiting for authorization...")
	spinner.Start()

	token, err := oauthClient.PollForToken(loginCtx, deviceResp.DeviceCode, deviceResp.Interval)
	if err != nil {
		spinner.StopWithError("Authorization failed")
		return nil, WrapSystemError(err, "authorization failed", "Make sure you entered the code at the URL above and approved the request.")
	}
	spinner.Stop()

	contentClient := github.NewContentClient(token.AccessToken)
	user, err := contentClient.FetchUser(loginCtx)
	if err != nil {
		return nil, WrapSystemError(err, "failed to fetch GitHub user info", "Authorization succeeded but user lookup failed. Try again.")
	}

	sess, err := saveGitHubSession(ctx, token, user)
	if err != nil {
		return nil, err
	}

	ui.PrintNewline()
	ui.PrintSuccess("Logged in as @%s", user.Login)

	installation, err := contentClient.HasAppInstallation(loginCtx, buildinfo.GitHubAppSlug)
	if err != nil {
		c.Logger.Debug("failed to check app installation", "error", err)
	} else if installation == nil {
		ui.PrintNewline()
		ui.PrintWarning("GitHub App not installed")
		ui.PrintDetail("To access your repositories, install the Stacktower app:")

		installURL := fmt.Sprintf("https://github.com/apps/%s/installations/new", buildinfo.GitHubAppSlug)
		ui.PrintKeyValue("URL", ui.StyleLink.Render(installURL))
		ui.PrintNewline()

		if err := openBrowser(installURL); err != nil {
			ui.PrintDetail("Copy the URL above and paste it in your browser")
		} else {
			ui.PrintDetail("Opening browser to install the app...")
		}
	} else {
		ui.PrintDetail("GitHub App installed for @%s", installation.Account.Login)
	}

	return sess, nil
}

func openBrowser(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("URL scheme must be http or https, got %q", parsed.Scheme)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", rawURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		return cmd.Process.Release()
	}
	return nil
}
