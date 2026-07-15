package mvmhost_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/mvm-sh/mvm/interp"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	hostImport        = "host"
	hostValueBinding  = "Value"
	hostValuesBinding = "Values"
)

type testDTO struct {
	Name  string
	Count int
}

type graphNode struct {
	Next *graphNode
}

type sharedGraph struct {
	Left  *graphNode
	Right *graphNode
}

type evalTestCase struct {
	wantValue    any
	wantCause    error
	name         string
	wantStdout   string
	wantKind     mvmhost.ErrorKind
	request      mvmhost.Request
	wantExitCode int
}

func newRequest(source string) mvmhost.Request {
	return mvmhost.Request{
		Bindings:    nil,
		Name:        "",
		Source:      source,
		SourceLimit: 0,
		OutputLimit: 0,
	}
}

func requestWithBindings(source string, bindings map[string]any) mvmhost.Request {
	request := newRequest(source)
	request.Bindings = mvmhost.Bindings{hostImport: bindings}

	return request
}

func valueCase(name string, request mvmhost.Request, value any) evalTestCase {
	return evalTestCase{
		wantValue:    value,
		wantCause:    nil,
		name:         name,
		wantStdout:   "",
		wantKind:     "",
		request:      request,
		wantExitCode: 0,
	}
}

func errorCase(name string, request mvmhost.Request, kind mvmhost.ErrorKind) evalTestCase {
	return evalTestCase{
		wantValue:    nil,
		wantCause:    nil,
		name:         name,
		wantStdout:   "",
		wantKind:     kind,
		request:      request,
		wantExitCode: 0,
	}
}

func valueTestCases() []evalTestCase {
	stdoutCase := valueCase(
		"stdout is separate from value",
		newRequest(`println("diagnostic"); 7`),
		7,
	)
	stdoutCase.wantStdout = "diagnostic\n"

	return []evalTestCase{
		valueCase("integer expression", newRequest(`1 + 2`), 3),
		valueCase("string value", newRequest(`"value"`), "value"),
		valueCase("boolean value", newRequest(`true`), true),
		valueCase(
			"host binding",
			requestWithBindings(`import "host"; host.Add(20, 22)`, map[string]any{
				"Add": func(left, right int) int { return left + right },
			}),
			42,
		),
		valueCase(
			"slice host value",
			requestWithBindings(`import "host"; host.Values()`, map[string]any{
				hostValuesBinding: func() []string { return []string{"a", "b"} },
			}),
			[]string{"a", "b"},
		),
		valueCase(
			"map host value",
			requestWithBindings(`import "host"; host.Values()`, map[string]any{
				hostValuesBinding: func() map[string]int { return map[string]int{"a": 1} },
			}),
			map[string]int{"a": 1},
		),
		valueCase(
			"nil host value",
			requestWithBindings(`import "host"; host.Value()`, map[string]any{
				hostValueBinding: func() any { return nil },
			}),
			nil,
		),
		valueCase(
			"DTO host value",
			requestWithBindings(`import "host"; host.Value()`, map[string]any{
				hostValueBinding: func() testDTO { return testDTO{Name: "item", Count: 2} },
			}),
			testDTO{Name: "item", Count: 2},
		),
		stdoutCase,
	}
}

func errorTestCases(hostCause error) []evalTestCase {
	hostErrorCase := errorCase(
		"host error",
		requestWithBindings(`import "host"; host.Fail()`, map[string]any{
			"Fail": func() error { return hostCause },
		}),
		mvmhost.ErrorKindRuntime,
	)
	hostErrorCase.wantCause = hostCause

	exitCase := errorCase(
		"virtualized exit",
		requestWithBindings(`import "host"; host.Exit(7)`, map[string]any{
			"Exit": func(code int) { panic(&interp.ExitError{Code: code}) },
		}),
		mvmhost.ErrorKindExit,
	)
	exitCase.wantExitCode = 7

	sourceLimitRequest := newRequest(`12345`)
	sourceLimitRequest.SourceLimit = 4
	outputLimitRequest := newRequest(`println("too long")`)
	outputLimitRequest.OutputLimit = 3
	outputLimitCase := errorCase("output limit", outputLimitRequest, mvmhost.ErrorKindOutputLimit)
	outputLimitCase.wantStdout = "too"

	unsupportedBinding := errorCase(
		"unsupported binding",
		requestWithBindings(`1`, map[string]any{"Values": make(chan int)}),
		mvmhost.ErrorKindRuntime,
	)
	nilBinding := errorCase(
		"nil binding",
		requestWithBindings(`1`, map[string]any{hostValueBinding: nil}),
		mvmhost.ErrorKindRuntime,
	)

	var nilFunction func()

	nilFunctionBinding := errorCase(
		"nil function binding",
		requestWithBindings(`1`, map[string]any{"Call": nilFunction}),
		mvmhost.ErrorKindRuntime,
	)

	return []evalTestCase{
		errorCase("syntax error", newRequest(`func {`), mvmhost.ErrorKindCompile),
		errorCase("runtime error", newRequest(`1 / 0`), mvmhost.ErrorKindRuntime),
		errorCase("panic", newRequest(`panic("boom")`), mvmhost.ErrorKindPanic),
		hostErrorCase,
		exitCase,
		unsupportedBinding,
		nilBinding,
		nilFunctionBinding,
		errorCase("source limit", sourceLimitRequest, mvmhost.ErrorKindSourceLimit),
		outputLimitCase,
		errorCase(
			"ambient import denied",
			newRequest(`import "example.invalid/ambient"; 1`),
			mvmhost.ErrorKindCompile,
		),
		errorCase(
			"embedded stdlib source import denied",
			newRequest(`import "slices"; slices.Clone([]int{1})`),
			mvmhost.ErrorKindCompile,
		),
	}
}

func TestEvaluator_Eval(t *testing.T) {
	t.Parallel()

	hostCause := errors.New("host failure")
	tests := append(valueTestCases(), errorTestCases(hostCause)...)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := mvmhost.New().Eval(context.Background(), test.request)
			assert.Equal(t, test.wantStdout, result.Stdout)

			if test.wantKind == "" {
				require.NoError(t, err)
				assert.Equal(t, test.wantValue, result.Value)

				return
			}

			require.Error(t, err)

			var evalErr *mvmhost.EvalError
			require.ErrorAs(t, err, &evalErr)
			assert.Equal(t, test.wantKind, evalErr.Kind)
			assert.Equal(t, test.wantExitCode, evalErr.ExitCode)

			if test.wantCause != nil {
				require.ErrorIs(t, err, test.wantCause)
			}
		})
	}
}

func TestEvaluator_EvalRejectsCyclicValueGraphs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		request func() mvmhost.Request
		name    string
	}{
		{
			name: "pointer binding",
			request: func() mvmhost.Request {
				node := &graphNode{Next: nil}
				node.Next = node

				return requestWithBindings(`1`, map[string]any{hostValueBinding: node})
			},
		},
		{
			name: "map binding",
			request: func() mvmhost.Request {
				value := make(map[string]any)
				value["self"] = value

				return requestWithBindings(`1`, map[string]any{hostValueBinding: value})
			},
		},
		{
			name: "slice binding",
			request: func() mvmhost.Request {
				value := make([]any, 1)
				value[0] = value

				return requestWithBindings(`1`, map[string]any{hostValueBinding: value})
			},
		},
		{
			name: "host result",
			request: func() mvmhost.Request {
				return requestWithBindings(`import "host"; host.Value()`, map[string]any{
					hostValueBinding: func() any {
						value := make(map[string]any)
						value["self"] = value

						return value
					},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := mvmhost.New().Eval(context.Background(), test.request())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cycle")

			var evalErr *mvmhost.EvalError
			require.ErrorAs(t, err, &evalErr)
			assert.Equal(t, mvmhost.ErrorKindRuntime, evalErr.Kind)
		})
	}
}

func TestEvaluator_EvalAllowsSharedAcyclicValues(t *testing.T) {
	t.Parallel()

	leaf := &graphNode{Next: nil}
	value := sharedGraph{Left: leaf, Right: leaf}
	request := requestWithBindings(`1`, map[string]any{hostValueBinding: value})

	result, err := mvmhost.New().Eval(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Value)
}

func TestEvaluator_EvalCancellation(t *testing.T) {
	t.Parallel()

	t.Run("before start", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mvmhost.New().Eval(ctx, newRequest(`1`))
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("cooperative host call", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		request := requestWithBindings(`import "host"; host.Wait()`, map[string]any{
			"Wait": func() error {
				close(started)
				<-ctx.Done()

				return ctx.Err()
			},
		})
		done := make(chan error, 1)

		go func() {
			_, err := mvmhost.New().Eval(ctx, request)
			done <- err
		}()

		<-started
		cancel()

		err := <-done
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)

		var evalErr *mvmhost.EvalError
		require.ErrorAs(t, err, &evalErr)
		assert.Equal(t, mvmhost.ErrorKindCanceled, evalErr.Kind)
	})
}

func TestEvaluator_ConcurrentInvocationsAreIsolated(t *testing.T) {
	t.Parallel()

	const invocationCount = 16

	var waitGroup sync.WaitGroup

	evaluator := mvmhost.New()

	errorsByInvocation := make(chan error, invocationCount)
	for index := range invocationCount {
		waitGroup.Go(func() {
			source := fmt.Sprintf("%d + 1", index)

			result, err := evaluator.Eval(context.Background(), newRequest(source))
			if err == nil && result.Value != index+1 {
				err = fmt.Errorf("value = %v, want %d", result.Value, index+1)
			}

			errorsByInvocation <- err
		})
	}

	waitGroup.Wait()
	close(errorsByInvocation)

	for err := range errorsByInvocation {
		require.NoError(t, err)
	}
}

func TestEvalError_ErrorIncludesZeroExitCode(t *testing.T) {
	t.Parallel()

	err := &mvmhost.EvalError{Err: errors.New("exit"), Kind: mvmhost.ErrorKindExit, ExitCode: 0}
	assert.Contains(t, err.Error(), "exit code 0")
}

func TestEvalError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	err := &mvmhost.EvalError{Err: cause, Kind: mvmhost.ErrorKindRuntime, ExitCode: 0}
	assert.ErrorIs(t, err, cause)
}
