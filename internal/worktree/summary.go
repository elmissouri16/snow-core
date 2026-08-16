package worktree

import (
	"context"
	"fmt"
	"strings"
)

// ReadOnlySummary returns a bounded status and diff-stat view without running a
// shell or changing repository state. A full patch is intentionally omitted.
func ReadOnlySummary(ctx context.Context, path string) (string, error) {
	git := gitRunner{}
	status, err := git.Run(ctx, path, "status", "--short", "--branch", "--untracked-files=normal")
	if err != nil {
		return "", err
	}
	if outputTruncated(status) {
		return "", ErrInventoryTooLarge
	}
	diff, err := git.Run(ctx, path, "diff", "--stat", "--no-ext-diff")
	if err != nil {
		return "", err
	}
	if outputTruncated(diff) {
		return "", ErrInventoryTooLarge
	}
	statusText := strings.TrimSpace(string(status))
	diffText := strings.TrimSpace(string(diff))
	if statusText == "" {
		statusText = "clean"
	}
	if diffText == "" {
		diffText = "no unstaged diff stat"
	}
	return fmt.Sprintf("Git status\n%s\n\nDiff stat\n%s", statusText, diffText), nil
}
