package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidateTasks(t *testing.T) {
	t.Parallel()

	valid := Load("").MustGet().Tasks
	tests := []struct {
		name    string
		mutate  func(*TaskRuntimeConfig)
		wantErr string
	}{
		{
			name:    "valid defaults",
			mutate:  func(*TaskRuntimeConfig) {},
			wantErr: "",
		},
		{
			name: "non-positive workers",
			mutate: func(tasks *TaskRuntimeConfig) {
				tasks.Workers = 0
			},
			wantErr: "config: task runtime bounds must be positive",
		},
		{
			name: "non-positive duration",
			mutate: func(tasks *TaskRuntimeConfig) {
				tasks.PollInterval = 0
			},
			wantErr: "config: task runtime bounds must be positive",
		},
		{
			name: "outcome limit below minimum",
			mutate: func(tasks *TaskRuntimeConfig) {
				tasks.MaxOutcomeBytes = minimumTaskOutcomeBytes - 1
			},
			wantErr: "config: tasks.max_outcome_bytes must be at least 256",
		},
		{
			name: "heartbeat equals lease",
			mutate: func(tasks *TaskRuntimeConfig) {
				tasks.Heartbeat = tasks.LeaseDuration
			},
			wantErr: "config: tasks.heartbeat_interval must be below lease_duration",
		},
		{
			name: "default timeout exceeds maximum",
			mutate: func(tasks *TaskRuntimeConfig) {
				tasks.DefaultTimeout = tasks.MaxTimeout + time.Second
			},
			wantErr: "config: tasks.default_timeout must not exceed max_timeout",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tasks := valid
			testCase.mutate(&tasks)

			config := Load("").MustGet()
			config.Tasks = tasks

			err := config.validateTasks()
			if testCase.wantErr == "" {
				require.NoError(t, err)

				return
			}

			assert.EqualError(t, err, testCase.wantErr)
		})
	}
}

func TestConfigIsDev(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  string
		want bool
	}{
		{name: envDevelopment, env: envDevelopment, want: true},
		{name: envTest, env: envTest, want: false},
		{name: envProduction, env: envProduction, want: false},
		{name: "empty", env: "", want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := Load("").MustGet()
			config.App.Env = testCase.env

			assert.Equal(t, testCase.want, config.IsDev())
		})
	}
}
