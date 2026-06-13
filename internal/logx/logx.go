// Package logx sets up file-based logging for the desktop GUI.
//
// The Windows release is built with -H windowsgui, which detaches the console,
// so anything written to stdout/stderr is discarded — making "Connect" failures
// impossible to diagnose. Setup() redirects the standard log package (and the
// process's stdout/stderr) to a file beside configs.json so the connect path,
// the routing commands, and xray's own error log are all captured.
package logx

import (
	"log"
	"os"
	"path/filepath"

	"mahsang/internal/store"
)

const (
	logName  = "mahsang.log"
	xrayName = "mahsang-xray.log"
)

// xrayLogPath is the file xray-core writes its own error log to, set by Setup.
// Empty until Setup runs (so the CLI and tests, which never call Setup, keep
// xray silent).
var xrayLogPath string

// Setup opens the log file, points the standard log package at it, and
// redirects os.Stdout/os.Stderr there too so library output and panics are
// captured. It returns the log file path (empty on failure). Safe to call once
// at startup; failures are non-fatal — the app still runs, just without a log.
func Setup() string {
	dir, err := store.Dir()
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, logName)
	// Start fresh each run, then open in append mode: the file has several
	// concurrent writers (the log package and tun2socks via os.Stderr), and
	// O_APPEND makes each write land at the end instead of clobbering by offset.
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ""
	}

	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	// Capture anything written to the process's stdout/stderr (e.g. panics)
	// into the same file; harmless when those handles are already valid.
	os.Stdout = f
	os.Stderr = f

	xrayLogPath = filepath.Join(dir, xrayName)
	// Start xray's log fresh each run so it only shows the current session.
	_ = os.Remove(xrayLogPath)

	log.Printf("=== mahsang started (log: %s) ===", path)
	return path
}

// XrayLogPath returns the path xray-core should write its error log to, or ""
// if Setup has not run (CLI/tests), in which case xray stays silent.
func XrayLogPath() string { return xrayLogPath }
