package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// RenderHelp renders styled help output for a cobra command.
func RenderHelp(cmd *cobra.Command) string {
	var b strings.Builder

	// Command name as title
	b.WriteString(StyleTitle.Render(cmd.Name()))
	b.WriteString("\n\n")

	// Long description or short if no long
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}
	if desc != "" {
		b.WriteString(styleDescription(desc))
		b.WriteString("\n\n")
	}

	// Usage section
	b.WriteString(RenderUsage(cmd))

	// Available commands
	if hasAvailableSubCommands(cmd) {
		b.WriteString(StyleHighlight.Render("Available Commands:"))
		b.WriteString("\n")
		b.WriteString(renderCommands(cmd))
		b.WriteString("\n")
	}

	// Flags
	if cmd.HasAvailableLocalFlags() {
		b.WriteString(StyleHighlight.Render("Flags:"))
		b.WriteString("\n")
		b.WriteString(renderFlags(cmd.LocalFlags()))
		b.WriteString("\n")
	}

	// Global/inherited flags
	if cmd.HasAvailableInheritedFlags() {
		b.WriteString(StyleHighlight.Render("Global Flags:"))
		b.WriteString("\n")
		b.WriteString(renderFlags(cmd.InheritedFlags()))
		b.WriteString("\n")
	}

	// Hint for subcommands
	if hasAvailableSubCommands(cmd) {
		hint := fmt.Sprintf("Use %s for more information about a command.",
			StyleCommand.Render(fmt.Sprintf("%q", cmd.CommandPath()+" [command] --help")))
		b.WriteString(StyleDim.Render(hint))
		b.WriteString("\n")
	}

	return b.String()
}

// RenderUsage renders styled usage output for a cobra command.
func RenderUsage(cmd *cobra.Command) string {
	var b strings.Builder

	b.WriteString(StyleHighlight.Render("Usage:"))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(StyleDim.Render(cmd.UseLine()))
	b.WriteString("\n")

	if hasAvailableSubCommands(cmd) {
		b.WriteString("  ")
		b.WriteString(StyleDim.Render(cmd.CommandPath() + " [command]"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return b.String()
}

// hasAvailableSubCommands returns true if the command has any available subcommands.
func hasAvailableSubCommands(cmd *cobra.Command) bool {
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			return true
		}
	}
	return false
}

// renderCommands renders the list of available subcommands with styling.
func renderCommands(cmd *cobra.Command) string {
	var b strings.Builder

	// Find the longest command name for padding
	maxLen := 0
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() && len(sub.Name()) > maxLen {
			maxLen = len(sub.Name())
		}
	}

	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		name := fmt.Sprintf("%-*s", maxLen, sub.Name())
		b.WriteString("  ")
		b.WriteString(StyleValue.Render(name))
		b.WriteString("   ")
		b.WriteString(StyleDim.Render(sub.Short))
		b.WriteString("\n")
	}

	return b.String()
}

// renderFlags renders flag usage with styling.
func renderFlags(flags *pflag.FlagSet) string {
	var b strings.Builder

	// Collect flag info for padding calculation
	type flagInfo struct {
		short    string
		name     string
		typeName string
		defValue string
		usage    string
	}

	var infos []flagInfo
	maxFlagLen := 0

	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}

		info := flagInfo{
			name:     f.Name,
			typeName: f.Value.Type(),
			defValue: f.DefValue,
			usage:    f.Usage,
		}
		if f.Shorthand != "" {
			info.short = f.Shorthand
		}

		// Calculate display length for padding
		flagLen := len(f.Name) + 2 // --name
		if f.Shorthand != "" {
			flagLen += 4 // -x,
		}
		if f.Value.Type() != "bool" {
			flagLen += len(f.Value.Type()) + 1 // space + type
		}
		if flagLen > maxFlagLen {
			maxFlagLen = flagLen
		}

		infos = append(infos, info)
	})

	for _, info := range infos {
		b.WriteString("  ")

		// Build the flag part
		var flagPart strings.Builder
		if info.short != "" {
			flagPart.WriteString("-")
			flagPart.WriteString(info.short)
			flagPart.WriteString(", ")
		} else {
			flagPart.WriteString("    ")
		}
		flagPart.WriteString("--")
		flagPart.WriteString(info.name)

		if info.typeName != "bool" {
			flagPart.WriteString(" ")
			flagPart.WriteString(info.typeName)
		}

		// Pad and render
		flagStr := fmt.Sprintf("%-*s", maxFlagLen+4, flagPart.String())
		b.WriteString(StyleCommand.Render(flagStr))
		b.WriteString(StyleDim.Render(info.usage))

		// Show default value if non-empty and not a bool
		if info.defValue != "" && info.defValue != "false" && info.defValue != "0" && info.defValue != "[]" {
			b.WriteString(StyleDim.Render(fmt.Sprintf(" (default %s)", info.defValue)))
		}

		b.WriteString("\n")
	}

	return b.String()
}

// styleDescription applies syntax highlighting to description text.
// It styles:
// - Section headers like "Examples:" in purple
// - Command examples (indented lines starting with the CLI name) in blue
// - Comments (text after #) in dim gray
// - Flags (--flag-name) in blue
func styleDescription(desc string) string {
	lines := strings.Split(desc, "\n")
	var result []string

	// Pattern for section headers like "Examples:", "Notes:", etc.
	headerPattern := regexp.MustCompile(`^([A-Z][a-z]+:)\s*$`)

	for _, line := range lines {
		// Check if it's a section header
		if headerPattern.MatchString(strings.TrimSpace(line)) {
			header := strings.TrimSpace(line)
			result = append(result, StyleHighlight.Render(header))
			continue
		}

		// Check if it's a command example (indented line starting with common CLI patterns)
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]

		if len(indent) >= 2 && looksLikeCommand(trimmed) {
			result = append(result, indent+styleCommandExample(trimmed))
			continue
		}

		// For regular text, style quotes and flags
		result = append(result, styleTextLine(line))
	}

	return strings.Join(result, "\n")
}

// styleTextLine styles a regular text line, handling quoted strings and flags.
func styleTextLine(line string) string {
	var result strings.Builder
	i := 0

	for i < len(line) {
		ch := line[i]

		// Check for quoted strings
		if ch == '\'' || ch == '"' {
			quote := ch
			end := findQuoteEnd(line, i+1, quote)
			if end > i {
				// Found a complete quoted string - style like commands (blue)
				quoted := line[i : end+1]
				result.WriteString(StyleCommand.Render(quoted))
				i = end + 1
				continue
			}
		}

		// Check for flags (--flag or -f) preceded by whitespace or at start
		if ch == '-' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			flagEnd := scanFlag(line, i)
			if flagEnd > i {
				flag := line[i:flagEnd]
				result.WriteString(StyleCommand.Render(flag))
				i = flagEnd
				continue
			}
		}

		result.WriteByte(ch)
		i++
	}

	return result.String()
}

// findQuoteEnd finds the closing quote position, returns -1 if not found.
func findQuoteEnd(line string, start int, quote byte) int {
	for i := start; i < len(line); i++ {
		if line[i] == quote {
			return i
		}
	}
	return -1
}

// scanFlag scans a flag starting at pos and returns the end position.
// Returns pos if not a valid flag.
func scanFlag(line string, pos int) int {
	i := pos

	// Must start with - or --
	if i >= len(line) || line[i] != '-' {
		return pos
	}
	i++

	// Optional second dash
	if i < len(line) && line[i] == '-' {
		i++
	}

	// Must have at least one letter
	if i >= len(line) || !isLetter(line[i]) {
		return pos
	}
	i++

	// Continue with letters, digits, or dashes
	for i < len(line) && (isLetter(line[i]) || isDigit(line[i]) || line[i] == '-') {
		i++
	}

	return i
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

// looksLikeCommand returns true if the line appears to be a command example.
func looksLikeCommand(line string) bool {
	// Common CLI tool prefixes for examples
	prefixes := []string{
		"stacktower ",
		"$ stacktower ",
		"$ ",
		"go ",
		"pip ",
		"npm ",
		"cargo ",
	}
	lower := strings.ToLower(line)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// styleCommandExample styles a command example line.
// Command part is styled in blue, comments after # are dimmed.
func styleCommandExample(line string) string {
	// Split on # for comment (but not inside quotes)
	commentIdx := findCommentStart(line)

	var cmdPart, commentPart string
	if commentIdx >= 0 {
		cmdPart = strings.TrimRight(line[:commentIdx], " ")
		commentPart = line[commentIdx:]
	} else {
		cmdPart = line
	}

	var b strings.Builder
	b.WriteString(StyleCommand.Render(cmdPart))
	if commentPart != "" {
		b.WriteString("  ")
		b.WriteString(StyleDim.Render(commentPart))
	}
	return b.String()
}

// findCommentStart finds the index of # that starts a comment (not inside quotes).
func findCommentStart(line string) int {
	inSingle := false
	inDouble := false
	for i, ch := range line {
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return i
			}
		}
	}
	return -1
}
