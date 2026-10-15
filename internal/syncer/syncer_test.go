package syncer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

type testMatcher map[string]bool

func (m testMatcher) Match(path string) bool { return m[path] }

func TestPlan(t *testing.T) {
	local := []LocalFile{
		{Path: "create.txt", FullPath: "/create", Size: 1, MD5: "aaa"},
		{Path: "same.txt", FullPath: "/same", Size: 2, MD5: "bbb"},
		{Path: "modify.txt", FullPath: "/modify", Size: 3, MD5: "ccc"},
	}
	remote := []RemoteObject{
		{Path: "same.txt", Size: 2, MD5: "\"BBB\""},
		{Path: "modify.txt", Size: 3, MD5: "ddd"},
		{Path: "remove.txt", Size: 4, MD5: "eee"},
		{Path: "ignored.txt", Size: 4, MD5: "fff"},
	}
	want := []Task{
		{Action: Create, Path: "create.txt", Local: "/create"},
		{Action: Modify, Path: "modify.txt", Local: "/modify"},
		{Action: Remove, Path: "remove.txt"},
	}
	got := Plan(local, remote, testMatcher{"ignored.txt": true}, false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan() = %#v, want %#v", got, want)
	}
}

func TestPlanForceCreatesEveryLocalFileAndStillRemovesRemoteOnlyFiles(t *testing.T) {
	local := []LocalFile{
		{Path: "same.txt", FullPath: "/same", Size: 2, MD5: "bbb"},
		{Path: "different.txt", FullPath: "/different", Size: 3, MD5: "ccc"},
	}
	remote := []RemoteObject{
		{Path: "same.txt", Size: 2, MD5: "bbb"},
		{Path: "different.txt", Size: 3, MD5: "ddd"},
		{Path: "remove.txt", Size: 4, MD5: "eee"},
	}
	want := []Task{
		{Action: Create, Path: "different.txt", Local: "/different"},
		{Action: Create, Path: "same.txt", Local: "/same"},
		{Action: Remove, Path: "remove.txt"},
	}
	got := Plan(local, remote, testMatcher{}, true)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan(force) = %#v, want %#v", got, want)
	}
}

type fakeStore struct {
	mu               sync.Mutex
	actions          []string
	deleteBatchSizes []int
	deleteErr        error
}

func (f *fakeStore) List(context.Context) ([]RemoteObject, error) { return nil, nil }
func (f *fakeStore) Upload(_ context.Context, path, _ string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, "upload "+path)
	return nil
}
func (f *fakeStore) DeleteBatch(_ context.Context, paths []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteBatchSizes = append(f.deleteBatchSizes, len(paths))
	for _, path := range paths {
		f.actions = append(f.actions, "delete "+path)
	}
	return f.deleteErr
}

func TestScanLocalSkipsIgnoredAndSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "keep.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	files, err := ScanLocal(root, testMatcher{"ignored.txt": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "keep.txt" || files[0].MD5 != "18ccf61d533b600bbf5a963359223fe4" {
		t.Fatalf("ScanLocal() = %#v", files)
	}
}

func TestExecute(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filename, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	type progressEvent struct {
		completed int
		total     int
		path      string
	}
	var progress []progressEvent
	tasks := []Task{
		{Action: Create, Path: "created.txt", Local: filename},
		{Action: Modify, Path: "modified.txt", Local: filename},
		{Action: Remove, Path: "removed.txt"},
	}
	if err := Execute(context.Background(), store, tasks, 1, func(completed, total int, task Task) {
		progress = append(progress, progressEvent{completed: completed, total: total, path: task.Path})
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"delete removed.txt", "upload created.txt", "upload modified.txt"}
	if !reflect.DeepEqual(store.actions, want) {
		t.Fatalf("actions = %#v, want %#v", store.actions, want)
	}
	wantProgress := []progressEvent{
		{0, 3, "removed.txt"}, {1, 3, "removed.txt"},
		{1, 3, "created.txt"}, {2, 3, "created.txt"},
		{2, 3, "modified.txt"}, {3, 3, "modified.txt"},
	}
	if !reflect.DeepEqual(progress, wantProgress) {
		t.Fatalf("progress = %#v, want %#v", progress, wantProgress)
	}
}

func TestExecuteBatchesRemovals(t *testing.T) {
	const total = 1601
	tasks := make([]Task, total)
	for index := range tasks {
		tasks[index] = Task{Action: Remove, Path: fmt.Sprintf("file-%04d", index)}
	}
	store := &fakeStore{}
	if err := Execute(context.Background(), store, tasks, 8, nil); err != nil {
		t.Fatal(err)
	}
	want := []int{800, 800, 1}
	got := append([]int(nil), store.deleteBatchSizes...)
	sort.Ints(got)
	sort.Ints(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delete batch sizes = %#v, want %#v", got, want)
	}
	if len(store.actions) != total {
		t.Fatalf("deleted %d objects, want %d", len(store.actions), total)
	}
}

type overlappingStore struct {
	deleteStarted chan struct{}
	releaseDelete chan struct{}
	uploadStarted chan struct{}
}

func (s *overlappingStore) List(context.Context) ([]RemoteObject, error) { return nil, nil }
func (s *overlappingStore) Upload(context.Context, string, string, string) error {
	close(s.uploadStarted)
	return nil
}
func (s *overlappingStore) DeleteBatch(ctx context.Context, _ []string) error {
	close(s.deleteStarted)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.releaseDelete:
		return nil
	}
}

func TestExecuteStartsUploadsWithoutWaitingForAllDeletes(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filename, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &overlappingStore{
		deleteStarted: make(chan struct{}),
		releaseDelete: make(chan struct{}),
		uploadStarted: make(chan struct{}),
	}
	tasks := []Task{
		{Action: Create, Path: "create.txt", Local: filename},
		{Action: Remove, Path: "remove.txt"},
	}
	done := make(chan error, 1)
	go func() {
		done <- Execute(context.Background(), store, tasks, 2, nil)
	}()

	select {
	case <-store.deleteStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete request")
	}
	select {
	case <-store.uploadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not start while delete request was still running")
	}
	close(store.releaseDelete)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for execution")
	}
}

type blockingStore struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	active  int
	maximum int
}

func (s *blockingStore) List(context.Context) ([]RemoteObject, error) { return nil, nil }
func (s *blockingStore) DeleteBatch(context.Context, []string) error  { return nil }
func (s *blockingStore) Upload(ctx context.Context, _ string, _ string, _ string) error {
	s.mu.Lock()
	s.active++
	if s.active > s.maximum {
		s.maximum = s.active
	}
	s.mu.Unlock()
	s.started <- struct{}{}
	select {
	case <-ctx.Done():
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
		return ctx.Err()
	case <-s.release:
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
		return nil
	}
}

func TestExecuteHonorsConcurrencyLimit(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filename, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks := make([]Task, 6)
	for index := range tasks {
		tasks[index] = Task{Action: Create, Path: fmt.Sprintf("file-%d", index), Local: filename}
	}
	store := &blockingStore{
		started: make(chan struct{}, len(tasks)),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- Execute(context.Background(), store, tasks, 3, nil)
	}()
	for range 3 {
		select {
		case <-store.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for workers")
		}
	}
	store.mu.Lock()
	maximum := store.maximum
	store.mu.Unlock()
	if maximum != 3 {
		t.Fatalf("maximum concurrency = %d, want 3", maximum)
	}
	select {
	case <-store.started:
		t.Fatal("more than three requests started before a worker was released")
	default:
	}
	close(store.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for execution")
	}
}
