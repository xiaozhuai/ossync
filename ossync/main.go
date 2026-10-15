package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/xiaozhuai/ossync/internal/config"
	ignorefile "github.com/xiaozhuai/ossync/internal/ignore"
	"github.com/xiaozhuai/ossync/internal/ossstore"
	"github.com/xiaozhuai/ossync/internal/syncer"
)

type options struct {
	directory       string
	config          string
	ignore          string
	showAll         bool
	concurrency     int
	dry             bool
	force           bool
	yes             bool
	allowEmpty      bool
	allowBucketRoot bool
}

const (
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
	colorReset  = "\x1b[0m"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stdout := newConsoleOutput(os.Stdout)
	stderr := newConsoleOutput(os.Stderr)
	command := newRootCommand(os.Stdin, stdout, stderr)
	if err := command.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintf(stderr, "\nCanceled.\n")
			os.Exit(130)
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := newRootCommand(stdin, stdout, stderr)
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

func newRootCommand(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	var opts options
	command := &cobra.Command{
		Use:           "ossync",
		Short:         "Synchronize a local directory to Alibaba Cloud OSS",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return synchronize(command.Context(), opts, command.InOrStdin(), command.OutOrStdout())
		},
	}
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetErr(stderr)
	flags := command.Flags()
	flags.BoolVarP(&opts.showAll, "all", "a", false, "show all changes")
	flags.IntVarP(&opts.concurrency, "concurrency", "j", 8, "maximum concurrent OSS requests")
	flags.StringVarP(&opts.directory, "dir", "d", ".", "local directory to synchronize")
	flags.StringVarP(&opts.config, "config", "c", "", "config file (default: <dir>/.ossyncconfig)")
	flags.StringVarP(&opts.ignore, "ignore", "i", "", "ignore file (default: <dir>/.ossyncignore)")
	flags.BoolVar(&opts.dry, "dry", false, "print tasks without making changes")
	flags.BoolVarP(&opts.force, "force", "f", false, "upload every local file as Create")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "execute without asking for confirmation")
	flags.BoolVar(&opts.allowEmpty, "allow-empty", false, "allow an empty local directory to remove remote objects")
	flags.BoolVar(&opts.allowBucketRoot, "allow-bucket-root", false, "allow changes at the bucket root")
	return command
}

func synchronize(ctx context.Context, opts options, stdin io.Reader, stdout io.Writer) error {
	if opts.concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1")
	}
	root, err := resolveRoot(opts.directory)
	if err != nil {
		return err
	}

	configPath, _, err := resolveAuxiliaryPath(root, opts.config, ".ossyncconfig")
	if err != nil {
		return err
	}
	cfg, target, err := config.Load(configPath)
	if err != nil {
		return err
	}
	ignorePath, ignoreExplicit, err := resolveAuxiliaryPath(root, opts.ignore, ".ossyncignore")
	if err != nil {
		return err
	}
	protected := []string{".ossyncconfig", ".ossyncignore"}
	for _, filename := range []string{configPath, ignorePath} {
		if rel, ok := relativeWithin(root, filename); ok {
			protected = append(protected, rel)
		}
	}
	matcher, err := ignorefile.Load(ignorePath, ignoreExplicit, protected)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s --> %s\n", root, cfg.Prefix)
	fmt.Fprintf(stdout, "Calculating changes...\n")
	local, err := syncer.ScanLocal(root, matcher)
	if err != nil {
		return err
	}
	store := ossstore.New(cfg, target)
	remote, err := store.List(ctx)
	if err != nil {
		return err
	}
	tasks := syncer.Plan(local, remote, matcher, opts.force)
	if len(tasks) > 0 {
		fmt.Fprintf(stdout, "\n")
	}
	printTasks(stdout, tasks, opts.showAll)
	counts := countChanges(tasks)
	if len(tasks) > 0 {
		fmt.Fprintf(stdout, "\n")
	}
	fmt.Fprintf(
		stdout,
		"Total Changes: %d (Create: %d, Modify: %d, Remove: %d)\n",
		len(tasks), counts.create, counts.modify, counts.remove,
	)
	if len(tasks) == 0 {
		fmt.Fprintf(stdout, "Already in sync.\n")
		return nil
	}
	if opts.dry {
		fmt.Fprintf(stdout, "Dry run: %d task(s), no changes made.\n", len(tasks))
		return nil
	}
	if err := validateApplySafety(opts, target, len(local), counts.remove); err != nil {
		return err
	}
	if !opts.yes {
		confirmed, err := confirm(ctx, stdin, stdout)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintf(stdout, "Canceled.\n")
			return nil
		}
	}
	fmt.Fprintf(stdout, "Executing changes...\n")
	progress := newTaskProgress(stdout)
	err = syncer.Execute(ctx, store, tasks, opts.concurrency, progress.Update)
	progress.Finish()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Synchronized %d task(s).\n", len(tasks))
	return nil
}

func resolveRoot(directory string) (string, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve local directory: %w", err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve local directory %q: %w", abs, err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("open local directory %q: %w", root, err)
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("local path %q is not a directory", root)
	}
	return root, nil
}

func validateApplySafety(opts options, target config.Target, localFiles, removals int) error {
	if target.Prefix == "" && !opts.allowBucketRoot {
		return fmt.Errorf("refusing to modify the bucket root without --allow-bucket-root")
	}
	if localFiles == 0 && removals > 0 && !opts.allowEmpty {
		return fmt.Errorf("refusing to remove %d remote object(s) from an empty local directory without --allow-empty", removals)
	}
	return nil
}

func printTasks(output io.Writer, tasks []syncer.Task, showAll bool) {
	const defaultLimit = 12
	displayed := tasks
	if !showAll && len(displayed) > defaultLimit {
		displayed = displayed[:defaultLimit]
	}
	for _, task := range displayed {
		color := actionColor(task.Action)
		fmt.Fprintf(output, "    %s%s %s%s\n", color, task.Action, task.Path, colorReset)
	}
	if len(displayed) < len(tasks) {
		fmt.Fprintf(output, "    ......\n")
	}
}

type changeCounts struct {
	create int
	modify int
	remove int
}

func countChanges(tasks []syncer.Task) changeCounts {
	var counts changeCounts
	for _, task := range tasks {
		switch task.Action {
		case syncer.Create:
			counts.create++
		case syncer.Modify:
			counts.modify++
		case syncer.Remove:
			counts.remove++
		}
	}
	return counts
}

type taskProgress struct {
	mu            sync.Mutex
	output        io.Writer
	interactive   bool
	active        bool
	lineWidth     int
	lastCompleted int
}

func newTaskProgress(output io.Writer) *taskProgress {
	interactive := true
	if value, ok := output.(interface{ IsTerminal() bool }); ok {
		interactive = value.IsTerminal()
	}
	return &taskProgress{output: output, interactive: interactive}
}

func (p *taskProgress) Update(completed, total int, _ syncer.Task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	const width = 30
	if total <= 0 {
		return
	}
	if completed < 0 {
		completed = 0
	}
	if completed > total {
		completed = total
	}
	filled := completed * width / total
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	line := fmt.Sprintf("[%s] %d/%d", bar, completed, total)
	if !p.interactive {
		if completed <= p.lastCompleted {
			return
		}
		fmt.Fprintf(p.output, "%s\n", line)
		p.lastCompleted = completed
		return
	}
	padding := p.lineWidth - len(line)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(p.output, "\r%s%s", line, strings.Repeat(" ", padding))
	if len(line) > p.lineWidth {
		p.lineWidth = len(line)
	}
	p.active = true
}

func (p *taskProgress) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive && p.active {
		fmt.Fprintf(p.output, "\n")
		p.active = false
	}
}

func actionColor(action syncer.Action) string {
	switch action {
	case syncer.Create:
		return colorGreen
	case syncer.Modify:
		return colorYellow
	case syncer.Remove:
		return colorRed
	default:
		return colorReset
	}
}

func resolveAuxiliaryPath(root, supplied, defaultName string) (string, bool, error) {
	explicit := supplied != ""
	if !explicit {
		supplied = filepath.Join(root, defaultName)
	}
	abs, err := filepath.Abs(supplied)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", defaultName, err)
	}
	return abs, explicit, nil
}

func relativeWithin(root, filename string) (string, bool) {
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func confirm(ctx context.Context, input io.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Apply these changes? [y/N]: ")
	type result struct {
		line string
		err  error
	}
	results := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		results <- result{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case read := <-results:
		if read.err != nil && !errors.Is(read.err, io.EOF) {
			return false, fmt.Errorf("read confirmation: %w", read.err)
		}
		answer := strings.ToLower(strings.TrimSpace(read.line))
		return answer == "y" || answer == "yes", nil
	}
}
