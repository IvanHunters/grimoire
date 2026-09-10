package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ivanohotnikov/markdown-editor/internal/claude/compact"
	"github.com/spf13/cobra"
)

// compactFlags mirrors compact.Options for the command line. It exists
// so the defaults can be asserted against the shared option set without
// executing cobra.
type compactFlags struct {
	keepRecentToolResults int
	keepRecentToolUses    int
	maxStubBytes          int
	keepRecentAttachments int
	keepArchives          int
	recompressStubs       bool
	restubTailBytes       int
	dropUsage             bool
	dropThinking          bool
	ledger                bool
}

func newCompactFlags() *compactFlags {
	d := compact.DefaultOptions()
	return &compactFlags{
		recompressStubs: d.RecompressStubs,
		restubTailBytes: d.RestubTailBytes,
		dropThinking:    d.DropThinking,
		dropUsage:       d.DropUsage,
		keepArchives:    3,
		ledger:          true,
	}
}

func (f *compactFlags) options() compact.Options {
	o := compact.DefaultOptions()
	o.KeepRecentToolResults = f.keepRecentToolResults
	o.KeepRecentToolUses = f.keepRecentToolUses
	o.MaxStubBytes = f.maxStubBytes
	o.KeepRecentAttachments = f.keepRecentAttachments
	o.KeepArchives = f.keepArchives
	o.RecompressStubs = f.recompressStubs
	o.RestubTailBytes = f.restubTailBytes
	o.DropThinking = f.dropThinking
	o.DropUsage = f.dropUsage
	return o
}

var compactFlagValues = newCompactFlags()

var compactCmd = &cobra.Command{
	Use:   "compact <transcript.jsonl>",
	Short: "Shrink a session transcript so it can be resumed again",
	Long: `Rewrites a Claude session transcript in place, evicting bulky tool
payloads from older turns and re-compressing stubs an earlier pass left
behind. The original is archived next to the file first, and a ledger of
every tool call is written alongside it.

The file is modified in place. To rehearse the result, copy the
transcript somewhere else and run this against the copy.`,
	Args: cobra.ExactArgs(1),
	RunE: runCompact,
}

func runCompact(cmd *cobra.Command, args []string) error {
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("transcript: %w", err)
	}

	// Leave the interface itself nil when no ledger is wanted: a typed
	// nil pointer would still be a non-nil io.Writer and panic on write.
	var ledger strings.Builder
	var ledgerOut io.Writer
	if compactFlagValues.ledger {
		ledgerOut = &ledger
	}

	res, err := compact.Compact(path, compactFlagValues.options(), ledgerOut)
	if err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	if compactFlagValues.ledger && ledger.Len() > 0 {
		ledgerPath := path + ".ledger.md"
		if err := os.WriteFile(ledgerPath, []byte(ledger.String()), 0o644); err != nil {
			return fmt.Errorf("write ledger: %w", err)
		}
		res.Stats.LedgerPath = ledgerPath
	}

	s := res.Stats
	out := cmd.OutOrStdout()
	if s.NoChange {
		_, err := fmt.Fprintf(out, "No change: nothing left to evict or re-compress in %s\n", path)
		return err
	}

	report := []string{
		fmt.Sprintf("Compacted %s", path),
		fmt.Sprintf("  lines:              %d", s.Lines),
		fmt.Sprintf("  tool_results:       %d total, %d evicted", s.ToolResults, s.ToolResultsEvicted),
		fmt.Sprintf("  tool_uses:          %d total, %d evicted", s.ToolUses, s.ToolUsesEvicted),
		fmt.Sprintf("  stubs recompressed: %d", s.StubsRecompressed),
		fmt.Sprintf("  thinking dropped:   %d", s.ThinkingBlocksDropped),
		fmt.Sprintf("  usage dropped:      %d", s.UsageBlocksDropped),
		fmt.Sprintf("  bytes:              %d to %d", s.BytesBefore, s.BytesAfter),
		fmt.Sprintf("  approx tokens:      %d to %d", s.ApproxTokensBefore, s.ApproxTokensAfter),
		fmt.Sprintf("  archive:            %s", s.ArchivePath),
	}
	if s.LedgerPath != "" {
		report = append(report, fmt.Sprintf("  ledger:             %s", s.LedgerPath))
	}
	_, err = fmt.Fprintln(out, strings.Join(report, "\n"))
	return err
}

func init() {
	f := compactCmd.Flags()
	v := compactFlagValues
	f.IntVar(&v.keepRecentToolResults, "keep-recent-tool-results", 0, "tool_result blocks to keep verbatim (0 uses the built-in default of 30)")
	f.IntVar(&v.keepRecentToolUses, "keep-recent-tool-uses", 0, "tool_use blocks to keep verbatim (0 follows keep-recent-tool-results)")
	f.IntVar(&v.maxStubBytes, "max-stub-bytes", 0, "tail kept inside a freshly written stub (0 uses the built-in default of 200)")
	f.IntVar(&v.keepRecentAttachments, "keep-recent-attachments", 0, "attachment events to keep (0 uses the built-in default of 40)")
	f.IntVar(&v.keepArchives, "keep-archives", v.keepArchives, "archives to retain next to the transcript; negative disables rotation")
	f.BoolVar(&v.recompressStubs, "recompress-stubs", v.recompressStubs, "also shrink stubs an earlier compact pass wrote")
	f.IntVar(&v.restubTailBytes, "restub-tail-bytes", v.restubTailBytes, "tail kept inside a re-compressed stub; 0 drops it")
	f.BoolVar(&v.dropThinking, "drop-thinking", v.dropThinking, "strip assistant thinking blocks")
	f.BoolVar(&v.dropUsage, "drop-usage", v.dropUsage, "strip message.usage; shrinks the file but not the prompt")
	f.BoolVar(&v.ledger, "ledger", v.ledger, "write a <transcript>.ledger.md sidecar")

	rootCmd.AddCommand(compactCmd)
}
