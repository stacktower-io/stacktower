package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
)

// cacheCommand creates the cache management command.
func (c *CLI) cacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local cache",
	}

	cmd.AddCommand(c.cacheClearCommand())
	cmd.AddCommand(c.cachePathCommand())
	cmd.AddCommand(c.cacheStatsCommand())

	return cmd
}

// cacheClearCommand creates the "cache clear" subcommand.
func (c *CLI) cacheClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear all cached HTTP responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cacheDir()
			if err != nil {
				return fmt.Errorf("get cache dir: %w", err)
			}

			if _, err := os.Stat(dir); os.IsNotExist(err) {
				ui.PrintInfo("Cache is empty")
				return nil
			}

			count := 0
			err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					slog.Debug("cache walk: skipping path", "path", path, "error", err)
					return nil
				}
				if path == dir {
					return nil
				}
				if !info.IsDir() {
					if err := os.Remove(path); err == nil {
						count++
					}
				}
				return nil
			})
			if err != nil {
				return err
			}

			// Clean up empty subdirectories
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					slog.Debug("cache walk: skipping path", "path", path, "error", err)
					return nil
				}
				if path == dir {
					return nil
				}
				if info.IsDir() {
					os.Remove(path)
				}
				return nil
			})

			ui.PrintSuccess("Cleared %d cached entries", count)
			ui.PrintDetail("Directory: %s", dir)
			return nil
		},
	}
}

// cachePathCommand creates the "cache path" subcommand.
func (c *CLI) cachePathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the cache directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cacheDir()
			if err != nil {
				return fmt.Errorf("get cache dir: %w", err)
			}
			// Bare stdout for scriptability: eval $(stacktower cache path)
			fmt.Println(dir)
			return nil
		},
	}
}

// cacheStatsCommand creates the "cache stats" subcommand.
func (c *CLI) cacheStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "stats",
		Aliases: []string{"info"},
		Short:   "Show cache size, entry count, and age",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cacheDir()
			if err != nil {
				return fmt.Errorf("get cache dir: %w", err)
			}

			if _, err := os.Stat(dir); os.IsNotExist(err) {
				ui.PrintInfo("Cache is empty")
				return nil
			}

			var (
				totalSize int64
				count     int
				oldest    time.Time
				newest    time.Time
			)

			err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				count++
				totalSize += info.Size()
				mod := info.ModTime()
				if oldest.IsZero() || mod.Before(oldest) {
					oldest = mod
				}
				if mod.After(newest) {
					newest = mod
				}
				return nil
			})
			if err != nil {
				return err
			}

			if count == 0 {
				ui.PrintInfo("Cache is empty")
				return nil
			}

			ui.PrintHeader("Cache")
			ui.PrintKeyValue("Directory", dir)
			ui.PrintKeyValue("Entries", fmt.Sprintf("%d", count))
			ui.PrintKeyValue("Total size", formatBytes(totalSize))
			ui.PrintKeyValue("Oldest", formatAge(oldest))
			ui.PrintKeyValue("Newest", formatAge(newest))
			return nil
		},
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatAge(t time.Time) string {
	age := time.Since(t)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(age.Hours()/24))
	}
}
