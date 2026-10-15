package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mattn/go-colorable"

	"ossync/internal/syncer"
)

func TestConfirm(t *testing.T) {
	for _, input := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		ok, err := confirm(context.Background(), strings.NewReader(input), &strings.Builder{})
		if err != nil || !ok {
			t.Fatalf("confirm(%q) = %v, %v", input, ok, err)
		}
	}
	for _, input := range []string{"\n", "n\n", "anything\n", ""} {
		ok, err := confirm(context.Background(), strings.NewReader(input), &strings.Builder{})
		if err != nil || ok {
			t.Fatalf("confirm(%q) = %v, %v", input, ok, err)
		}
	}
}

func TestConfirmCanBeCanceled(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := confirm(ctx, reader, &strings.Builder{})
	if ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("confirm canceled = %v, %v", ok, err)
	}
}

func TestRelativeWithin(t *testing.T) {
	if got, ok := relativeWithin("/tmp/root", "/tmp/root/a/b"); !ok || got != "a/b" {
		t.Fatalf("relativeWithin child = %q, %v", got, ok)
	}
	if _, ok := relativeWithin("/tmp/root", "/tmp/other"); ok {
		t.Fatal("relativeWithin accepted outside path")
	}
}

func TestHelpCombinesShortAndLongFlags(t *testing.T) {
	var stdout, stderr strings.Builder
	err := run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"-a, --all",
		"-c, --config string",
		"-d, --dir string",
		"-f, --force",
		"-i, --ignore string",
		"-j, --concurrency int",
		"-y, --yes",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("help does not contain %q:\n%s", expected, stdout.String())
		}
	}
}

func TestConcurrencyMustBePositive(t *testing.T) {
	var stdout, stderr strings.Builder
	err := run(
		context.Background(),
		[]string{"--concurrency", "0"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil || !strings.Contains(err.Error(), "--concurrency must be at least 1") {
		t.Fatalf("run() error = %v", err)
	}
}

func TestPrintTasksUsesIndentationAndColors(t *testing.T) {
	var output strings.Builder
	printTasks(&output, []syncer.Task{
		{Action: syncer.Create, Path: "new.txt"},
		{Action: syncer.Modify, Path: "changed.txt"},
		{Action: syncer.Remove, Path: "old.txt"},
	}, false)
	want := "    \x1b[32mCreate new.txt\x1b[0m\n" +
		"    \x1b[33mModify changed.txt\x1b[0m\n" +
		"    \x1b[31mRemove old.txt\x1b[0m\n"
	if output.String() != want {
		t.Fatalf("printTasks() = %q, want %q", output.String(), want)
	}
}

func TestPrintTasksStripsColorsWhenRedirected(t *testing.T) {
	var output bytes.Buffer
	redirected := &consoleOutput{
		Writer:   colorable.NewNonColorable(&output),
		terminal: false,
	}
	printTasks(redirected, []syncer.Task{{Action: syncer.Remove, Path: "old.txt"}}, false)
	if want := "    Remove old.txt\n"; output.String() != want {
		t.Fatalf("redirected tasks = %q, want %q", output.String(), want)
	}
}

func TestPrintTasksLimitsChangesByDefault(t *testing.T) {
	tasks := make([]syncer.Task, 13)
	for index := range tasks {
		tasks[index] = syncer.Task{Action: syncer.Create, Path: fmt.Sprintf("file-%02d", index+1)}
	}

	var limited strings.Builder
	printTasks(&limited, tasks, false)
	if strings.Count(limited.String(), "Create file-") != 12 || !strings.Contains(limited.String(), "    ......\n") {
		t.Fatalf("limited changes output = %q", limited.String())
	}
	if strings.Contains(limited.String(), "file-13") {
		t.Fatalf("limited output contains the thirteenth change: %q", limited.String())
	}

	var all strings.Builder
	printTasks(&all, tasks, true)
	if strings.Count(all.String(), "Create file-") != 13 || strings.Contains(all.String(), "......") {
		t.Fatalf("all changes output = %q", all.String())
	}
}

func TestCountChanges(t *testing.T) {
	counts := countChanges([]syncer.Task{
		{Action: syncer.Create},
		{Action: syncer.Create},
		{Action: syncer.Modify},
		{Action: syncer.Remove},
	})
	want := changeCounts{create: 2, modify: 1, remove: 1}
	if counts != want {
		t.Fatalf("countChanges() = %#v, want %#v", counts, want)
	}
}

func TestTaskProgress(t *testing.T) {
	var output strings.Builder
	progress := &taskProgress{output: &output, interactive: true}
	task := syncer.Task{Action: syncer.Modify, Path: "assets/app.js"}
	progress.Update(1, 2, task)
	progress.Update(2, 2, task)
	progress.Finish()

	want := "\r[===============               ] 1/2" +
		"\r[==============================] 2/2\n"
	if output.String() != want {
		t.Fatalf("progress output = %q, want %q", output.String(), want)
	}
}

func TestTaskProgressWhenRedirected(t *testing.T) {
	var output bytes.Buffer
	redirected := &consoleOutput{
		Writer:   colorable.NewNonColorable(&output),
		terminal: false,
	}
	progress := newTaskProgress(redirected)
	task := syncer.Task{Action: syncer.Create, Path: "index.html"}
	progress.Update(0, 2, task)
	progress.Update(1, 2, task)
	progress.Update(1, 2, task)
	progress.Update(2, 2, task)
	progress.Finish()

	want := "[===============               ] 1/2\n" +
		"[==============================] 2/2\n"
	if output.String() != want {
		t.Fatalf("redirected progress = %q, want %q", output.String(), want)
	}
}
