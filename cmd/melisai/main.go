// melisai — comprehensive Linux system performance analyzer.
//
// Uses BPF/eBPF tools, procfs/sysfs, and standard utilities to produce
// structured JSON reports optimized for AI-driven diagnostics.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmitriimaksimovdevelop/melisai/internal/collector"
	diffpkg "github.com/dmitriimaksimovdevelop/melisai/internal/diff"
	"github.com/dmitriimaksimovdevelop/melisai/internal/ebpf"
	"github.com/dmitriimaksimovdevelop/melisai/internal/installer"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
	"github.com/dmitriimaksimovdevelop/melisai/internal/orchestrator"
	"github.com/dmitriimaksimovdevelop/melisai/internal/output"
)

var (
	version = "0.1.0"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "melisai",
		Short: "Comprehensive Linux system performance analyzer",
		Long: `melisai — single Go binary for Linux performance analysis.

Collects metrics via BPF/eBPF tools (Brendan Gregg's ecosystem),
procfs/sysfs, and standard utilities. Produces structured JSON
reports optimized for AI-driven diagnostics and optimization.

Tier 1: /proc, /sys, ss, dmesg (always works, no root)
Tier 2: BCC tools — runqlat, biolatency, tcpconnlat, etc. (needs root + tools)
Tier 3: Native eBPF (cilium/ebpf) — zero dependencies (needs root + kernel ≥ 4.15)`,
		Version: version,
	}

	// --- collect command ---
	var (
		collectProfile   string
		collectFocus     string
		collectOutput    string
		collectAIPrompt  bool
		collectDuration  string
		collectPID       int
		collectCgroup    string
		collectMaxEvents int
		collectQuiet     bool
		collectVerbose   bool
	)

	collectCmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect system performance metrics",
		Long:  "Run all available collectors and produce a structured JSON report.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := collector.DefaultConfig()
			cfg.Profile = collectProfile
			cfg.Quiet = collectQuiet

			if collectFocus != "" {
				cfg.Focus = strings.Split(collectFocus, ",")
			}
			if collectPID > 0 {
				cfg.TargetPIDs = []int{collectPID}
			}
			if collectCgroup != "" {
				cfg.TargetCgroups = []string{collectCgroup}
			}
			if collectMaxEvents > 0 {
				cfg.MaxEventsPerCollector = collectMaxEvents
			}

			// Override duration from profile
			profile := orchestrator.GetProfile(cfg.Profile)
			cfg.Duration = profile.Duration

			// Duration override via flag
			if collectDuration != "" {
				d, err := parseDuration(collectDuration)
				if err != nil {
					return fmt.Errorf("invalid duration: %w", err)
				}
				cfg.Duration = d
			}

			ctx := context.Background()

			// Register all Tier 1 collectors
			collectors := orchestrator.RegisterCollectors(cfg)
			if len(collectors) == 0 {
				return fmt.Errorf("no collectors available (this is a bug)")
			}

			orch := orchestrator.New(collectors, cfg)
			report, err := orch.Run(ctx)
			if err != nil {
				return err
			}

			// Optionally add AI prompt
			if collectAIPrompt {
				report.AIContext = buildAIContext(report)
			}

			return output.WriteJSON(report, collectOutput)
		},
	}

	collectCmd.Flags().StringVarP(&collectProfile, "profile", "p", "standard", "Collection profile: quick, standard, deep")
	collectCmd.Flags().StringVarP(&collectFocus, "focus", "f", "", "Focus areas: stacks,network,disk (comma-separated)")
	collectCmd.Flags().StringVarP(&collectOutput, "output", "o", "-", "Output file path (- for stdout)")
	collectCmd.Flags().BoolVar(&collectAIPrompt, "ai-prompt", false, "Include AI analysis prompt in output")
	collectCmd.Flags().StringVar(&collectDuration, "duration", "", "Override profile duration (e.g. 15s, 1m)")
	collectCmd.Flags().IntVar(&collectPID, "pid", 0, "Filter to specific PID")
	collectCmd.Flags().StringVar(&collectCgroup, "cgroup", "", "Filter to specific cgroup path")
	collectCmd.Flags().IntVar(&collectMaxEvents, "max-events", 1000, "Max events per collector")
	collectCmd.Flags().BoolVarP(&collectQuiet, "quiet", "q", false, "Suppress progress output")
	collectCmd.Flags().BoolVarP(&collectVerbose, "verbose", "v", false, "Enable debug logging")

	// --- install command ---
	var installDryRun bool

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install BPF tools and dependencies",
		Long:  "Detect the Linux distribution and install bcc-tools, bpftrace, perf, FlameGraph.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(installDryRun)
		},
	}
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Show what would be installed")

	// --- capabilities command ---
	capabilitiesCmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Show available tools and system capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapabilities()
		},
	}

	// --- diff command ---
	var diffOutput string

	diffCmd := &cobra.Command{
		Use:   "diff <baseline.json> <current.json>",
		Short: "Compare two melisai reports",
		Long:  "Produce a diff report showing USE deltas, new/resolved anomalies, histogram changes.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], args[1], diffOutput)
		},
	}
	diffCmd.Flags().StringVarP(&diffOutput, "output", "o", "-", "Output diff file path")

	rootCmd.AddCommand(collectCmd, installCmd, capabilitiesCmd, diffCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// parseDuration parses a human-friendly duration string.
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// buildAIContext constructs the AI analysis prompt.
func buildAIContext(report *model.Report) *model.AIContext {
	return output.GenerateAIPrompt(report)
}

// runInstall handles the `install` command.
func runInstall(dryRun bool) error {
	inst := &installer.Installer{DryRun: dryRun}
	return inst.Run()
}

// runCapabilities handles the `capabilities` command.
func runCapabilities() error {
	caps := ebpf.DetectBPFCapabilities()
	fmt.Print(ebpf.FormatCapabilities(caps))

	btfInfo := ebpf.DetectBTF()
	fmt.Printf("Kernel: %s\n", btfInfo.KernelVersion)
	fmt.Printf("BTF: %v\n", btfInfo.Available)
	fmt.Printf("CO-RE: %v\n", btfInfo.CORESupport)
	return nil
}

// runDiff handles the `diff` command.
func runDiff(baselinePath, currentPath, outputPath string) error {
	baseline, err := diffpkg.LoadReport(baselinePath)
	if err != nil {
		return fmt.Errorf("load baseline: %w", err)
	}
	current, err := diffpkg.LoadReport(currentPath)
	if err != nil {
		return fmt.Errorf("load current: %w", err)
	}

	result := diffpkg.Compare(baseline, current)

	if outputPath == "-" {
		// Print human-readable diff
		fmt.Print(diffpkg.FormatDiff(result))
	} else {
		// Write JSON diff
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(outputPath, data, 0644)
	}
	return nil
}
