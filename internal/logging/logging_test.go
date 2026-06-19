// SPDX-License-Identifier: BSD-3-Clause

package logging

import (
	"testing"

	"github.com/go-openapi/testify/v2/require"
	"go.uber.org/zap/zapcore"
)

func TestOptionsLevels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level string
		want  zapcore.Level
	}{
		{"", zapcore.InfoLevel},
		{"info", zapcore.InfoLevel},
		{"INFO", zapcore.InfoLevel},
		{"debug", zapcore.DebugLevel},
		{"warn", zapcore.WarnLevel},
		{"warning", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"0", zapcore.InfoLevel},
		{"1", zapcore.Level(-1)},
		{"3", zapcore.Level(-3)},
	}
	for _, tc := range cases {
		opts, err := Options(tc.level, "console")
		require.NoError(t, err, "level %q", tc.level)
		require.Equal(t, tc.want, opts.Level, "level %q", tc.level)
	}
}

func TestOptionsFormat(t *testing.T) {
	t.Parallel()

	// json (structured) is the default; console is development mode.
	jsonOpts, err := Options("info", "JSON")
	require.NoError(t, err)
	require.False(t, jsonOpts.Development)

	defaultOpts, err := Options("info", "")
	require.NoError(t, err)
	require.False(t, defaultOpts.Development)

	consoleOpts, err := Options("info", "console")
	require.NoError(t, err)
	require.True(t, consoleOpts.Development)
}

func TestOptionsStacktrace(t *testing.T) {
	t.Parallel()

	// Routine errors stay one line until the level is debug or more verbose.
	infoOpts, err := Options("info", "console")
	require.NoError(t, err)
	require.Equal(t, zapcore.PanicLevel, infoOpts.StacktraceLevel)

	debugOpts, err := Options("debug", "console")
	require.NoError(t, err)
	require.Equal(t, zapcore.ErrorLevel, debugOpts.StacktraceLevel)

	verboseOpts, err := Options("2", "console")
	require.NoError(t, err)
	require.Equal(t, zapcore.ErrorLevel, verboseOpts.StacktraceLevel)
}

func TestOptionsInvalid(t *testing.T) {
	t.Parallel()

	_, err := Options("loud", "console")
	require.Error(t, err)

	_, err = Options("-1", "console")
	require.Error(t, err)

	_, err = Options("info", "xml")
	require.Error(t, err)
}
