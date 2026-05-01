package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// completionCommand creates the completion command for generating shell completions.
// The receiver is unused here but kept for API consistency — all command
// constructors are methods on *CLI so RootCommand() can wire them uniformly.
func (c *CLI) completionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for stacktower.

To load completions:

Bash:
  $ source <(stacktower completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ stacktower completion bash > /etc/bash_completion.d/stacktower
  # macOS:
  $ stacktower completion bash > $(brew --prefix)/etc/bash_completion.d/stacktower

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ stacktower completion zsh > "${fpath[1]}/_stacktower"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ stacktower completion fish | source

  # To load completions for each session, execute once:
  $ stacktower completion fish > ~/.config/fish/completions/stacktower.fish

PowerShell:
  PS> stacktower completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> stacktower completion powershell > stacktower.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				// cobra.OnlyValidArgs prevents reaching here, but return
				// a clear error defensively.
				return NewUserError("unknown shell: "+args[0], "Supported: bash, zsh, fish, powershell")
			}
		},
	}

	return cmd
}
