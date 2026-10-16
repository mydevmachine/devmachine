package scheduled

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNotFound means the workspace has no task with that id.
var ErrNotFound = errors.New("no such task")

// Store is the workspace's folder of tasks and runs.
type Store struct{ Dir string }

// Open opens the store under home, creating its folders.
func Open(home string) (Store, error) {
	s := Store{Dir: filepath.Join(home, ".devmachine", "scheduled")}
	for _, d := range []string{"tasks", "runs", "locks"} {
		if err := os.MkdirAll(filepath.Join(s.Dir, d), 0o700); err != nil {
			return Store{}, fmt.Errorf("creating the scheduled folder: %w", err)
		}
	}
	return s, nil
}

func checkTaskID(id string) error {
	if !ValidTaskID(id) {
		return fmt.Errorf("%q is not a task id, such as t_7f3a9c", id)
	}
	return nil
}

func checkRunID(id string) error {
	if !ValidRunID(id) {
		return fmt.Errorf("%q is not a run id, such as 20261015T120000Z-ab12", id)
	}
	return nil
}

func (s Store) taskPath(id string) string { return filepath.Join(s.Dir, "tasks", id+".json") }
func (s Store) runsDir(id string) string  { return filepath.Join(s.Dir, "runs", id) }

// LockPath is the file a running task holds.
func (s Store) LockPath(id string) string { return filepath.Join(s.Dir, "locks", id+".pid") }

// LogPath is where a run's output goes.
func (s Store) LogPath(taskID, runID string) string {
	return filepath.Join(s.runsDir(taskID), runID+".log")
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", filepath.Base(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Put writes a task, replacing one with the same id.
func (s Store) Put(t Task) error {
	if err := checkTaskID(t.ID); err != nil {
		return err
	}
	return writeJSONFile(s.taskPath(t.ID), t)
}

// Get reads one task.
func (s Store) Get(id string) (Task, error) {
	if err := checkTaskID(id); err != nil {
		return Task{}, err
	}
	var t Task
	err := readJSONFile(s.taskPath(id), &t)
	if errors.Is(err, fs.ErrNotExist) {
		return Task{}, fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	return t, err
}

// List reads every task, sorted by name.
func (s Store) List() ([]Task, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "tasks"))
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	tasks := []Task{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !ValidTaskID(id) {
			continue
		}
		t, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Name != tasks[j].Name {
			return tasks[i].Name < tasks[j].Name
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

// Delete removes a task and every record of its runs.
func (s Store) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	if err := os.RemoveAll(s.runsDir(id)); err != nil {
		return fmt.Errorf("removing the runs of %s: %w", id, err)
	}
	if err := os.Remove(s.taskPath(id)); err != nil {
		return fmt.Errorf("removing %s: %w", id, err)
	}
	return nil
}

// PutRun writes a run's record.
func (s Store) PutRun(r Run) error {
	if err := checkTaskID(r.TaskID); err != nil {
		return err
	}
	if err := checkRunID(r.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.runsDir(r.TaskID), 0o700); err != nil {
		return fmt.Errorf("creating the runs folder: %w", err)
	}
	return writeJSONFile(filepath.Join(s.runsDir(r.TaskID), r.ID+".json"), r)
}

// Runs reads a task's runs, newest first.
func (s Store) Runs(taskID string) ([]Run, error) {
	if err := checkTaskID(taskID); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.runsDir(taskID))
	if errors.Is(err, fs.ErrNotExist) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	runs := []Run{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !ValidRunID(id) {
			continue
		}
		var r Run
		if err := readJSONFile(filepath.Join(s.runsDir(taskID), e.Name()), &r); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID > runs[j].ID })
	return runs, nil
}

// LastRun is a task's newest run, or nil when it never ran.
func (s Store) LastRun(taskID string) (*Run, error) {
	runs, err := s.Runs(taskID)
	if err != nil || len(runs) == 0 {
		return nil, err
	}
	return &runs[0], nil
}

// ReadLog reads what a run printed.
func (s Store) ReadLog(taskID, runID string) ([]byte, error) {
	if err := checkTaskID(taskID); err != nil {
		return nil, err
	}
	if err := checkRunID(runID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.LogPath(taskID, runID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("run %s has no output", runID)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the output of %s: %w", runID, err)
	}
	return data, nil
}

// Prune keeps a task's newest keep runs and removes the rest.
func (s Store) Prune(taskID string, keep int) error {
	runs, err := s.Runs(taskID)
	if err != nil {
		return err
	}
	for _, r := range runs[min(keep, len(runs)):] {
		for _, p := range []string{filepath.Join(s.runsDir(taskID), r.ID+".json"), s.LogPath(taskID, r.ID)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("removing an old run: %w", err)
			}
		}
	}
	return nil
}

type tickState struct {
	LastTick time.Time `json:"last_tick"`
}

// LastTick is when the timer last ran, or zero when it never did.
func (s Store) LastTick() (time.Time, error) {
	var st tickState
	err := readJSONFile(filepath.Join(s.Dir, "state.json"), &st)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	return st.LastTick, err
}

// SetLastTick records when the timer ran.
func (s Store) SetLastTick(t time.Time) error {
	return writeJSONFile(filepath.Join(s.Dir, "state.json"), tickState{LastTick: t})
}
