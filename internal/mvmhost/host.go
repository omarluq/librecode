// Package mvmhost provides a narrow adapter around the MVM interpreter.
package mvmhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"runtime"

	"github.com/mvm-sh/mvm/interp"
	"github.com/mvm-sh/mvm/lang/golang"
	"github.com/mvm-sh/mvm/vm"
	"github.com/samber/oops"
)

const (
	defaultSourceLimit = 256 << 10
	defaultOutputLimit = 1 << 20
)

// Bindings maps an import path to the Go values available to interpreted code.
type Bindings map[string]map[string]any

// Request describes one isolated MVM evaluation.
type Request struct {
	Bindings    Bindings
	Name        string
	Source      string
	SourceLimit int
	OutputLimit int
}

// Result contains the evaluated value and separately captured diagnostics.
type Result struct {
	Value  any
	Stdout string
	Stderr string
}

// ErrorKind classifies an evaluation failure.
type ErrorKind string

// Evaluation error kinds exposed by the host boundary.
const (
	ErrorKindCompile     ErrorKind = "compile"
	ErrorKindRuntime     ErrorKind = "runtime"
	ErrorKindPanic       ErrorKind = "panic"
	ErrorKindExit        ErrorKind = "exit"
	ErrorKindCanceled    ErrorKind = "canceled"
	ErrorKindSourceLimit ErrorKind = "source_limit"
	ErrorKindOutputLimit ErrorKind = "output_limit"
)

// EvalError is a normalized MVM evaluation error.
type EvalError struct {
	Err      error
	Kind     ErrorKind
	ExitCode int
}

func (e *EvalError) Error() string {
	if e.Kind == ErrorKindExit {
		return fmt.Sprintf("mvm %s error (exit code %d): %v", e.Kind, e.ExitCode, e.Err)
	}

	return fmt.Sprintf("mvm %s error: %v", e.Kind, e.Err)
}

func (e *EvalError) Unwrap() error { return e.Err }

// Evaluator executes source using a fresh interpreter for every call.
type Evaluator struct{}

// New creates an MVM evaluator.
func New() *Evaluator { return &Evaluator{} }

// Eval evaluates source synchronously. MVM v0.5.0 cannot interrupt arbitrary
// bytecode, so the context is checked before evaluation and by host bindings;
// untrusted source must invoke this adapter in a killable helper process.
func (e *Evaluator) Eval(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, evalError(ErrorKindCanceled, 0, err)
	}

	sourceLimit := effectiveLimit(request.SourceLimit, defaultSourceLimit)
	if len(request.Source) > sourceLimit {
		err := fmt.Errorf("source is %d bytes; limit is %d", len(request.Source), sourceLimit)

		return Result{}, evalError(ErrorKindSourceLimit, 0, err)
	}

	outputLimit := effectiveLimit(request.OutputLimit, defaultOutputLimit)
	outputBudget := &limitedOutputBudget{remaining: outputLimit, overflow: false}
	stdout := newLimitedBuffer(outputBudget)
	stderr := newLimitedBuffer(outputBudget)
	value, evalErr := evaluate(request, stdout, stderr)
	result := Result{Value: nil, Stdout: stdout.String(), Stderr: stderr.String()}

	if stdout.Overflowed() || stderr.Overflowed() {
		return result, evalError(ErrorKindOutputLimit, 0, errOutputLimit)
	}

	if evalErr != nil {
		return result, normalizeError(evalErr)
	}

	if err := ctx.Err(); err != nil {
		return result, evalError(ErrorKindCanceled, 0, err)
	}

	return finalizeResult(result, value)
}

func finalizeResult(result Result, value reflect.Value) (Result, error) {
	if !value.IsValid() {
		return result, nil
	}

	result.Value = value.Interface()
	if hostErr, ok := result.Value.(error); ok && hostErr != nil {
		return result, evalError(ErrorKindRuntime, 0, hostErr)
	}

	if err := validateValue(value, "result"); err != nil {
		return result, evalError(ErrorKindRuntime, 0, err)
	}

	return result, nil
}

func effectiveLimit(configured, fallback int) int {
	if configured > 0 {
		return configured
	}

	return fallback
}

func evaluate(request Request, stdout, stderr io.Writer) (reflect.Value, error) {
	machine := interp.NewInterpreter(golang.GoSpec)
	machine.SetIO(bytes.NewReader(nil), stdout, stderr)
	// os.DevNull is a portable non-directory path, so package lookups cannot
	// fall back to the working directory. Disable MVM's embedded source-package
	// and remote fallbacks as well; imports are limited to explicit bindings.
	machine.SetPkgfs(os.DevNull)
	machine.SetStdlibFS(emptyFS{})
	machine.SetRemoteFS(emptyFS{})

	bindings, err := reflectBindings(request.Bindings)
	if err != nil {
		return reflect.Value{}, err
	}

	machine.ImportPackageValues(bindings)

	name := request.Name
	if name == "" {
		name = "execute.go"
	}

	value, err := machine.Eval(name, request.Source)
	if err != nil {
		return value, oops.In("mvmhost").Wrapf(err, "run MVM source")
	}

	return value, nil
}

func reflectBindings(bindings Bindings) (map[string]map[string]reflect.Value, error) {
	values := make(map[string]map[string]reflect.Value, len(bindings))
	for packageName, packageBindings := range bindings {
		items := make(map[string]reflect.Value, len(packageBindings))
		for name, value := range packageBindings {
			reflected := reflect.ValueOf(value)
			if !reflected.IsValid() {
				return nil, &valueError{message: fmt.Sprintf("binding %q.%q is nil", packageName, name)}
			}

			if err := validateBinding(reflected, packageName+"."+name); err != nil {
				return nil, err
			}

			items[name] = reflected
		}

		values[packageName] = items
	}

	return values, nil
}

func validateBinding(value reflect.Value, name string) error {
	if value.Kind() == reflect.Func {
		if value.IsNil() {
			return &valueError{message: fmt.Sprintf("binding %q is a nil function", name)}
		}

		return nil
	}

	return validateValue(value, "binding "+name)
}

func validateValue(value reflect.Value, path string) error {
	if !value.IsValid() {
		return &valueError{message: path + " is invalid"}
	}

	switch value.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return nil
	case reflect.Interface, reflect.Pointer:
		return validateIndirect(value, path)
	case reflect.Array, reflect.Slice:
		return validateSequence(value, path)
	case reflect.Map:
		return validateMap(value, path)
	case reflect.Struct:
		return validateStruct(value, path)
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return &valueError{message: fmt.Sprintf("%s has unsupported type %s", path, value.Type())}
	}

	return &valueError{message: fmt.Sprintf("%s has unsupported type %s", path, value.Type())}
}

func validateIndirect(value reflect.Value, path string) error {
	if value.IsNil() {
		return nil
	}

	return validateValue(value.Elem(), path)
}

func validateSequence(value reflect.Value, path string) error {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return nil
	}

	for index := range value.Len() {
		if err := validateValue(value.Index(index), fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}

	return nil
}

func validateMap(value reflect.Value, path string) error {
	if value.IsNil() {
		return nil
	}

	iterator := value.MapRange()
	for iterator.Next() {
		if err := validateValue(iterator.Key(), path+" map key"); err != nil {
			return err
		}

		if err := validateValue(iterator.Value(), path+" map value"); err != nil {
			return err
		}
	}

	return nil
}

func validateStruct(value reflect.Value, path string) error {
	typeOfValue := value.Type()
	for index := range value.NumField() {
		field := typeOfValue.Field(index)
		if !field.IsExported() {
			continue
		}

		if err := validateValue(value.Field(index), path+"."+field.Name); err != nil {
			return err
		}
	}

	return nil
}

func normalizeError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return evalError(ErrorKindCanceled, 0, err)
	}

	var exitErr *interp.ExitError
	if errors.As(err, &exitErr) {
		return evalError(ErrorKindExit, exitErr.Code, err)
	}

	var valueErr *valueError
	if errors.As(err, &valueErr) {
		return evalError(ErrorKindRuntime, 0, err)
	}

	var panicErr *vm.PanicError
	if errors.As(err, &panicErr) {
		if _, ok := panicErr.Raw.(runtime.Error); ok {
			return evalError(ErrorKindRuntime, 0, err)
		}

		return evalError(ErrorKindPanic, 0, err)
	}
	// MVM reports parser/compiler failures as ordinary errors.
	return evalError(ErrorKindCompile, 0, err)
}

type valueError struct{ message string }

func (e *valueError) Error() string { return e.message }

func evalError(kind ErrorKind, exitCode int, err error) error {
	normalized := &EvalError{Err: err, Kind: kind, ExitCode: exitCode}

	return oops.In("mvmhost").Code(string(kind)).Wrapf(normalized, "evaluate MVM source")
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

var errOutputLimit = errors.New("MVM output limit exceeded")

type limitedOutputBudget struct {
	remaining int
	overflow  bool
}

type limitedBuffer struct {
	budget *limitedOutputBudget
	buffer bytes.Buffer
}

func newLimitedBuffer(budget *limitedOutputBudget) *limitedBuffer {
	return &limitedBuffer{buffer: bytes.Buffer{}, budget: budget}
}

func (w *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) <= w.budget.remaining {
		written, err := w.buffer.Write(data)
		w.budget.remaining -= written

		if err != nil {
			return written, oops.In("mvmhost").Wrapf(err, "capture MVM output")
		}

		return written, nil
	}

	allowed := w.budget.remaining
	if allowed > 0 {
		written, err := w.buffer.Write(data[:allowed])
		w.budget.remaining -= written

		if err != nil {
			return written, oops.In("mvmhost").Wrapf(err, "capture bounded MVM output")
		}

		allowed = written
	}

	w.budget.overflow = true

	return allowed, errOutputLimit
}

func (w *limitedBuffer) String() string   { return w.buffer.String() }
func (w *limitedBuffer) Overflowed() bool { return w.budget.overflow }

var _ io.Writer = (*limitedBuffer)(nil)
