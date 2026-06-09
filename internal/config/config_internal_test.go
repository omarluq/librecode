package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
