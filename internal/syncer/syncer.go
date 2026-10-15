package syncer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Action string

const (
	Create Action = "Create"
	Modify Action = "Modify"
	Remove Action = "Remove"
)

type LocalFile struct {
	Path     string
	FullPath string
	Size     int64
	MD5      string
}

type RemoteObject struct {
	Path string
	Size int64
	MD5  string
}

type Task struct {
	Action Action
	Path   string
	Local  string
}

type Store interface {
	List(context.Context) ([]RemoteObject, error)
	Upload(context.Context, string, string, string) error
	DeleteBatch(context.Context, []string) error
}

type Matcher interface {
	Match(string) bool
}

type ProgressFunc func(completed, total int, task Task)

const deleteBatchSize = 800

func ScanLocal(root string, matcher Matcher) ([]LocalFile, error) {
	files := make([]LocalFile, 0)
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if matcher.Match(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		digest, err := fileMD5(filename)
		if err != nil {
			return fmt.Errorf("hash %q: %w", filename, err)
		}
		files = append(files, LocalFile{
			Path: relative, FullPath: filename, Size: info.Size(), MD5: digest,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan local directory %q: %w", root, err)
	}
	return files, nil
}

func Plan(local []LocalFile, remote []RemoteObject, matcher Matcher, force bool) []Task {
	localByPath := make(map[string]LocalFile, len(local))
	remoteByPath := make(map[string]RemoteObject, len(remote))
	for _, file := range local {
		localByPath[file.Path] = file
	}
	for _, object := range remote {
		if object.Path == "" || strings.HasSuffix(object.Path, "/") || matcher.Match(object.Path) {
			continue
		}
		remoteByPath[object.Path] = object
	}

	tasks := make([]Task, 0)
	for path, file := range localByPath {
		if force {
			tasks = append(tasks, Task{Action: Create, Path: path, Local: file.FullPath})
			continue
		}
		object, exists := remoteByPath[path]
		if !exists {
			tasks = append(tasks, Task{Action: Create, Path: path, Local: file.FullPath})
			continue
		}
		if file.Size != object.Size || !strings.EqualFold(file.MD5, normalizeETag(object.MD5)) {
			tasks = append(tasks, Task{Action: Modify, Path: path, Local: file.FullPath})
		}
	}
	for path := range remoteByPath {
		if _, exists := localByPath[path]; !exists {
			tasks = append(tasks, Task{Action: Remove, Path: path})
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		left, right := actionRank(tasks[i].Action), actionRank(tasks[j].Action)
		if left == right {
			return tasks[i].Path < tasks[j].Path
		}
		return left < right
	})
	return tasks
}

func actionRank(action Action) int {
	switch action {
	case Create:
		return 0
	case Modify:
		return 1
	case Remove:
		return 2
	default:
		return 3
	}
}

func Execute(ctx context.Context, store Store, tasks []Task, concurrency int, progress ProgressFunc) error {
	if concurrency < 1 {
		return fmt.Errorf("concurrency must be at least 1")
	}

	uploads, removals, err := splitTasks(tasks)
	if err != nil {
		return err
	}
	tracker := executionProgress{total: len(tasks), callback: progress}

	deleteBatches := makeDeleteBatches(removals, deleteBatchSize)
	jobs := make([]executionJob, 0, len(deleteBatches)+len(uploads))
	for _, batch := range deleteBatches {
		jobs = append(jobs, executionJob{tasks: batch})
	}
	for _, task := range uploads {
		jobs = append(jobs, executionJob{tasks: []Task{task}})
	}

	// Removal batches are submitted first, followed immediately by uploads.
	// There is deliberately no barrier between the two kinds of request.
	return runPool(ctx, concurrency, jobs, func(ctx context.Context, job executionJob) error {
		task := job.tasks[0]
		tracker.show(task)
		if task.Action == Remove {
			paths := make([]string, len(job.tasks))
			for index, removal := range job.tasks {
				paths[index] = removal.Path
			}
			if err := store.DeleteBatch(ctx, paths); err != nil {
				return fmt.Errorf("Remove batch starting at %q: %w", task.Path, err)
			}
			tracker.advance(len(job.tasks), job.tasks[len(job.tasks)-1])
			return nil
		}

		md5sum, err := fileMD5(task.Local)
		if err != nil {
			return fmt.Errorf("rehash %q before upload: %w", task.Path, err)
		}
		if err := store.Upload(ctx, task.Path, task.Local, md5sum); err != nil {
			return fmt.Errorf("%s %q: %w", task.Action, task.Path, err)
		}
		tracker.advance(1, task)
		return nil
	})
}

type executionJob struct {
	tasks []Task
}

func splitTasks(tasks []Task) (uploads, removals []Task, err error) {
	for _, task := range tasks {
		switch task.Action {
		case Create, Modify:
			uploads = append(uploads, task)
		case Remove:
			removals = append(removals, task)
		default:
			return nil, nil, fmt.Errorf("unknown action %q", task.Action)
		}
	}
	return uploads, removals, nil
}

func makeDeleteBatches(tasks []Task, size int) [][]Task {
	if size < 1 {
		return nil
	}
	batches := make([][]Task, 0, (len(tasks)+size-1)/size)
	for start := 0; start < len(tasks); start += size {
		end := start + size
		if end > len(tasks) {
			end = len(tasks)
		}
		batches = append(batches, tasks[start:end])
	}
	return batches
}

type executionProgress struct {
	mu        sync.Mutex
	completed int
	total     int
	callback  ProgressFunc
}

func (p *executionProgress) show(task Task) {
	if p.callback == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callback(p.completed, p.total, task)
}

func (p *executionProgress) advance(count int, task Task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed += count
	if p.callback != nil {
		p.callback(p.completed, p.total, task)
	}
}

func runPool[T any](ctx context.Context, concurrency int, jobs []T, run func(context.Context, T) error) error {
	if len(jobs) == 0 {
		return nil
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobChannel := make(chan T)
	var workers sync.WaitGroup
	var firstError error
	var errorOnce sync.Once
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-workerCtx.Done():
					return
				case job, ok := <-jobChannel:
					if !ok {
						return
					}
					if err := run(workerCtx, job); err != nil {
						errorOnce.Do(func() {
							firstError = err
							cancel()
						})
						return
					}
				}
			}
		}()
	}

sendJobs:
	for _, job := range jobs {
		select {
		case <-workerCtx.Done():
			break sendJobs
		case jobChannel <- job:
		}
	}
	close(jobChannel)
	workers.Wait()
	if firstError != nil {
		return firstError
	}
	return ctx.Err()
}

func fileMD5(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"")
}
