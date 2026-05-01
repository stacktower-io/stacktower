package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
)

// cacheDirAndStats gathers the cache directory and its current stats, or
// reports an empty cache. It is the shared prelude for `cache clear` and
// `cache stats` so we can't disagree about what "empty" means.
func cacheDirAndStats() (dir string, stats cacheStats, hasEntries bool, err error) {
	dir, err = cacheDir()
	if err != nil {
		return "", cacheStats{}, false, WrapSystemError(err, "failed to determine cache directory", "Check that your home directory is accessible.")
	}
	stats, hasEntries, err = collectCacheStats(dir)
	if err != nil {
		return dir, stats, false, WrapSystemError(err, "failed to walk cache directory", "Check filesystem permissions on the cache path.")
	}
	return dir, stats, hasEntries, nil
}

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
//
// We count entries up-front (via collectCacheStats) and then recreate the
// cache directory from scratch with os.RemoveAll + MkdirAll. This is much
// simpler than a manual walk and atomically handles nested registry
// directories, symlinks, etc.
func (c *CLI) cacheClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear all cached HTTP responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, stats, hasEntries, err := cacheDirAndStats()
			if err != nil {
				return err
			}

			if !hasEntries {
				ui.PrintInfo("Cache is empty")
				return nil
			}

			if err := os.RemoveAll(dir); err != nil {
				return WrapSystemError(err, "failed to clear cache", "Check filesystem permissions on the cache path.")
			}
			// Recreate the directory so the next cache hit doesn't fail on a
			// missing path. 0o700 matches what cache.NewFileCache would use on
			// first use.
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return WrapSystemError(err, "cache cleared but failed to recreate cache directory", "Check filesystem permissions on the cache path.")
			}

			ui.PrintSuccess("Cleared %d cached entries", stats.Entries)
			ui.PrintDetail("Directory: %s", dir)
			return nil
		},
	}
}

// cacheStats holds the fields rendered by `cache stats` in both text
// and JSON modes.
type cacheStats struct {
	Directory string    `json:"directory"`
	Entries   int       `json:"entries"`
	TotalSize int64     `json:"total_size_bytes"`
	Oldest    time.Time `json:"oldest,omitempty"`
	Newest    time.Time `json:"newest,omitempty"`
}

// collectCacheStats walks the cache directory once, stats each file entry,
// and returns aggregated counts and timestamps.
func collectCacheStats(dir string) (cacheStats, bool, error) {
	stats := cacheStats{Directory: dir}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return stats, false, nil
	}

	var skipped int
	var firstSkipped string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			skipped++
			if firstSkipped == "" {
				firstSkipped = path
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			skipped++
			if firstSkipped == "" {
				firstSkipped = path
			}
			return nil
		}
		stats.Entries++
		stats.TotalSize += info.Size()
		mod := info.ModTime()
		if stats.Oldest.IsZero() || mod.Before(stats.Oldest) {
			stats.Oldest = mod
		}
		if mod.After(stats.Newest) {
			stats.Newest = mod
		}
		return nil
	})
	if err != nil {
		return stats, false, err
	}
	if skipped > 0 {
		ui.PrintWarning("Skipped %d cache entries (permission error or broken symlink, e.g. %s)", skipped, firstSkipped)
	}
	return stats, stats.Entries > 0, nil
}

// cachePathCommand creates the "cache path" subcommand.
func (c *CLI) cachePathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the cache directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cacheDir()
			if err != nil {
				return WrapSystemError(err, "failed to determine cache directory", "Check that your home directory is accessible.")
			}
			// Bare stdout for scriptability: eval $(stacktower cache path)
			fmt.Println(dir)
			return nil
		},
	}
}

// cacheStatsCommand creates the "cache stats" subcommand.
func (c *CLI) cacheStatsCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:     "stats",
		Aliases: []string{"info"},
		Short:   "Show cache size, entry count, and age",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, stats, hasEntries, err := cacheDirAndStats()
			if err != nil {
				return err
			}

			switch format {
			case FormatJSON:
				return encodeJSON(os.Stdout, stats)
			case "", FormatText:
				if !hasEntries {
					ui.PrintInfo("Cache is empty")
					ui.PrintKeyValue("Directory", dir)
					return nil
				}
				ui.PrintHeader("Cache")
				ui.PrintKeyValue("Directory", stats.Directory)
				ui.PrintKeyValue("Entries", fmt.Sprintf("%d", stats.Entries))
				ui.PrintKeyValue("Total size", ui.FormatBytes(stats.TotalSize))
				ui.PrintKeyValue("Oldest", ui.FormatAge(stats.Oldest))
				ui.PrintKeyValue("Newest", ui.FormatAge(stats.Newest))
				return nil
			default:
				return unsupportedFormatError(format, nil)
			}
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: text (default), json")
	return cmd
}
