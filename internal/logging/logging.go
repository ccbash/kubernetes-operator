// SPDX-License-Identifier: BSD-3-Clause

// Package logging builds the operator's logger configuration from a small,
// operator-friendly set of options: a human-readable log level and an output
// format, both switchable from a flag or Helm value.
package logging

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options builds controller-runtime zap options from a log level and output
// format, giving a clean, switchable logging setup like other Kubernetes
// operators.
//
// level is one of debug, info, warn (warning), error, or a non-negative integer
// for higher debug verbosity (1 == debug, 2, 3, … map to logr V-levels, so
// "--log-level=2" shows V(2) and below).
//
// format is json (structured, the default — matches Flux and other
// controller-runtime operators) or console (human-readable, for local runs).
//
// Stack traces are recorded only for panics, so routine errors stay a single
// line; running at debug level (or more verbose) lowers that to error level so
// stack traces are available while debugging.
func Options(level, format string) (zap.Options, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return zap.Options{}, err
	}

	opts := zap.Options{
		Level: lvl,
		// ISO8601 millisecond timestamps (e.g. "2026-06-19T10:21:44.661Z"),
		// matching the structured-log style of other controller-runtime operators
		// (Flux, cert-manager, …) rather than zap's default epoch float.
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		opts.Development = false
	case "console":
		opts.Development = true
	default:
		return zap.Options{}, fmt.Errorf("invalid log format %q: must be \"json\" or \"console\"", format)
	}

	opts.StacktraceLevel = zapcore.PanicLevel
	if lvl <= zapcore.DebugLevel {
		opts.StacktraceLevel = zapcore.ErrorLevel
	}

	return opts, nil
}

// parseLevel maps a level name (or a non-negative integer verbosity) to the
// corresponding zap level. logr V(n) logging is enabled at zap level -n, so a
// numeric n is mapped to zapcore.Level(-n).
func parseLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	}

	if n, err := strconv.Atoi(strings.TrimSpace(level)); err == nil && n >= 0 {
		return zapcore.Level(-n), nil
	}

	return 0, fmt.Errorf("invalid log level %q: must be debug, info, warn, error, or a non-negative integer", level)
}
