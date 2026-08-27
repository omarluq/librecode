// Package startupprofile provides opt-in diagnostics for application startup.
package startupprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

const (
	profilePathEnv   = "LIBRECODE_STARTUP_PROFILE"
	tracePathEnv     = "LIBRECODE_STARTUP_TRACE"
	cpuPathEnv       = "LIBRECODE_STARTUP_CPU_PROFILE"
	profileFileMode  = 0o600
	profileEventKind = "startup"
)

type contextKey struct{}

type event struct {
	Name       string `json:"name"`
	SinceStart int64  `json:"since_start_ns"`
	Duration   int64  `json:"duration_ns,omitempty"`
}

type report struct {
	Events []event `json:"events"`
}

// Profiler records startup milestones and owns optional runtime profiles.
type Profiler struct {
	started      time.Time
	traceFile    *os.File
	cpuFile      *os.File
	profilePath  string
	events       []event
	lock         sync.Mutex
	traceRunning bool
	cpuRunning   bool
	enabled      bool
	finished     bool
}

// Start creates diagnostics configured through LIBRECODE_STARTUP_* environment variables.
func Start() (*Profiler, error) {
	profiler := &Profiler{
		started: time.Time{}, events: nil,
		profilePath: os.Getenv(profilePathEnv),
		traceFile:   nil, cpuFile: nil, traceRunning: false, cpuRunning: false,
		enabled: false, finished: false, lock: sync.Mutex{},
	}
	tracePath := os.Getenv(tracePathEnv)
	cpuPath := os.Getenv(cpuPathEnv)

	profiler.enabled = profiler.profilePath != "" || tracePath != "" || cpuPath != ""
	if !profiler.enabled {
		return profiler, nil
	}

	profiler.started = time.Now()
	profiler.events = append(profiler.events, event{Name: "startup", SinceStart: 0, Duration: 0})

	if err := profiler.startTrace(tracePath); err != nil {
		return nil, err
	}

	if err := profiler.startCPU(cpuPath); err != nil {
		return nil, errors.Join(err, profiler.stopTrace())
	}

	return profiler, nil
}

// Context attaches profiler to ctx.
func Context(ctx context.Context, profiler *Profiler) context.Context {
	return context.WithValue(ctx, contextKey{}, profiler)
}

// FromContext returns the attached profiler or nil.
func FromContext(ctx context.Context) *Profiler {
	value := ctx.Value(contextKey{})

	profiler, ok := value.(*Profiler)
	if !ok {
		return nil
	}

	return profiler
}

func (profiler *Profiler) startTrace(path string) error {
	if path == "" {
		return nil
	}

	file, err := createProfileFile(path)
	if err != nil {
		return fmt.Errorf("create startup trace: %w", err)
	}

	if err := trace.Start(file); err != nil {
		return errors.Join(fmt.Errorf("start runtime trace: %w", err), file.Close())
	}

	profiler.traceFile = file
	profiler.traceRunning = true

	return nil
}

func (profiler *Profiler) startCPU(path string) error {
	if path == "" {
		return nil
	}

	file, err := createProfileFile(path)
	if err != nil {
		return fmt.Errorf("create startup CPU profile: %w", err)
	}

	if err := pprof.StartCPUProfile(file); err != nil {
		return errors.Join(fmt.Errorf("start CPU profile: %w", err), file.Close())
	}

	profiler.cpuFile = file
	profiler.cpuRunning = true

	return nil
}

// Mark records an instantaneous startup milestone.
func (profiler *Profiler) Mark(name string) {
	if profiler == nil {
		return
	}

	profiler.lock.Lock()
	defer profiler.lock.Unlock()

	if !profiler.enabled || profiler.finished {
		return
	}

	profiler.events = append(profiler.events, event{
		Name: name, SinceStart: time.Since(profiler.started).Nanoseconds(), Duration: 0,
	})
	if profiler.traceRunning {
		trace.Log(context.Background(), profileEventKind, name)
	}
}

// Span records a timed startup stage. The returned function must be called once.
func (profiler *Profiler) Span(name string) func() {
	if profiler == nil {
		return noopSpan
	}

	profiler.lock.Lock()
	enabled := profiler.enabled && !profiler.finished
	profiler.lock.Unlock()

	if !enabled {
		return noopSpan
	}

	started := time.Now()
	region := trace.StartRegion(context.Background(), profileEventKind+"."+name)

	var once sync.Once

	return func() {
		once.Do(func() {
			region.End()
			profiler.recordSpan(name, started)
		})
	}
}

func noopSpan() {
	// Intentionally empty so callers can always defer the result of Span.
}

func (profiler *Profiler) recordSpan(name string, started time.Time) {
	profiler.lock.Lock()
	defer profiler.lock.Unlock()

	if !profiler.enabled || profiler.finished {
		return
	}

	profiler.events = append(profiler.events, event{
		Name:       name,
		SinceStart: started.Sub(profiler.started).Nanoseconds(),
		Duration:   time.Since(started).Nanoseconds(),
	})
}

// FirstFrame records the first completed visible frame and finalizes startup profiles.
func (profiler *Profiler) FirstFrame() error {
	if profiler == nil {
		return nil
	}

	profiler.lock.Lock()
	defer profiler.lock.Unlock()

	if !profiler.enabled || profiler.finished {
		return nil
	}

	profiler.events = append(profiler.events, event{
		Name: "first_frame", SinceStart: time.Since(profiler.started).Nanoseconds(), Duration: 0,
	})

	return profiler.finish()
}

// Stop finalizes active runtime profiles when startup exits before its first frame.
func (profiler *Profiler) Stop() error {
	if profiler == nil {
		return nil
	}

	profiler.lock.Lock()
	defer profiler.lock.Unlock()

	if !profiler.enabled || profiler.finished {
		return nil
	}

	return profiler.finish()
}

func (profiler *Profiler) finish() error {
	profiler.finished = true

	return errors.Join(profiler.stopCPU(), profiler.stopTrace(), profiler.writeReport())
}

func (profiler *Profiler) stopTrace() error {
	if profiler.traceRunning {
		trace.Stop()

		profiler.traceRunning = false
	}

	return closeProfileFile(&profiler.traceFile)
}

func (profiler *Profiler) stopCPU() error {
	if profiler.cpuRunning {
		pprof.StopCPUProfile()

		profiler.cpuRunning = false
	}

	return closeProfileFile(&profiler.cpuFile)
}

func (profiler *Profiler) writeReport() error {
	if profiler.profilePath == "" {
		return nil
	}

	contents, err := json.MarshalIndent(report{Events: profiler.events}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode startup report: %w", err)
	}

	contents = append(contents, '\n')
	if err := writeProfileFile(profiler.profilePath, contents); err != nil {
		return fmt.Errorf("write startup report: %w", err)
	}

	return nil
}

func createProfileFile(path string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open profile directory: %w", err)
	}

	file, createErr := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, profileFileMode)

	closeErr := root.Close()
	if createErr != nil {
		return nil, errors.Join(fmt.Errorf("create profile file: %w", createErr), closeErr)
	}

	if closeErr != nil {
		return nil, errors.Join(fmt.Errorf("close profile directory: %w", closeErr), file.Close())
	}

	if err := file.Chmod(profileFileMode); err != nil {
		return nil, errors.Join(fmt.Errorf("secure profile file: %w", err), file.Close())
	}

	return file, nil
}

func writeProfileFile(path string, contents []byte) error {
	file, err := createProfileFile(path)
	if err != nil {
		return err
	}

	_, writeErr := file.Write(contents)

	return errors.Join(writeErr, file.Close())
}

func closeProfileFile(file **os.File) error {
	if *file == nil {
		return nil
	}

	err := (*file).Close()
	*file = nil

	if err != nil {
		return fmt.Errorf("close profile file: %w", err)
	}

	return nil
}
