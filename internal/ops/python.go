package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxPythonOutputBytes = 64 * 1024

type PythonResult struct {
	OK       bool   `json:"ok"`
	Script   string `json:"script"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

func resolvePythonBinary() string {
	if envBin := strings.TrimSpace(os.Getenv("PYTHON_BIN")); envBin != "" {
		return envBin
	}
	if p, err := exec.LookPath("python"); err == nil {
		return p
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return "python"
}

func validatePythonScriptPath(scriptsDir, scriptName string) (string, string, error) {
	cleanedName := strings.TrimSpace(scriptName)
	if cleanedName == "" {
		return "", "", errors.New("script name is required")
	}

	if !strings.EqualFold(filepath.Ext(cleanedName), ".py") {
		return "", "", errors.New("only .py scripts are allowed")
	}

	absBase, err := filepath.Abs(scriptsDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve scripts directory: %w", err)
	}
	realBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		realBase = absBase
	}

	targetPath := filepath.Join(realBase, filepath.FromSlash(cleanedName))
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve script path: %w", err)
	}

	rel, err := filepath.Rel(realBase, absTarget)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", errors.New("script path escapes allowed directory")
	}

	info, err := os.Stat(absTarget)
	if err != nil {
		return "", "", fmt.Errorf("script not found: %w", err)
	}
	if info.IsDir() {
		return "", "", errors.New("script path cannot be a directory")
	}

	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return "", "", fmt.Errorf("failed to evaluate script path: %w", err)
	}

	relReal, err := filepath.Rel(realBase, realTarget)
	if err != nil || relReal == "." || strings.HasPrefix(relReal, "..") || filepath.IsAbs(relReal) {
		return "", "", errors.New("resolved script path escapes allowed directory")
	}

	return realTarget, filepath.ToSlash(relReal), nil
}

func appendBounded(buf *bytes.Buffer, chunk []byte, limit int, truncated *bool) {
	if buf.Len() >= limit {
		*truncated = true
		return
	}
	remaining := limit - buf.Len()
	if len(chunk) > remaining {
		buf.Write(chunk[:remaining])
		*truncated = true
		return
	}
	buf.Write(chunk)
}

type boundedWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	appendBounded(&w.buf, p, w.limit, &w.truncated)
	return len(p), nil
}

func RunPythonScript(scriptsDir, scriptName string, args []string, timeout time.Duration) PythonResult {
	start := time.Now()

	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	scriptPath, safeRelName, err := validatePythonScriptPath(scriptsDir, scriptName)
	if err != nil {
		return PythonResult{
			OK:       false,
			Script:   scriptName,
			Duration: time.Since(start).String(),
			Error:    err.Error(),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pyBin := resolvePythonBinary()
	cmdArgs := append([]string{"-B", scriptPath}, args...)
	cmd := exec.CommandContext(ctx, pyBin, cmdArgs...)

	var outBuf boundedWriter
	outBuf.limit = maxPythonOutputBytes

	var errBuf boundedWriter
	errBuf.limit = maxPythonOutputBytes

	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	duration := time.Since(start).String()

	stdoutStr := outBuf.buf.String()
	if outBuf.truncated {
		stdoutStr += "\n[stdout truncated at 64KB]"
	}

	stderrStr := errBuf.buf.String()
	if errBuf.truncated {
		stderrStr += "\n[stderr truncated at 64KB]"
	}

	if ctx.Err() == context.DeadlineExceeded {
		return PythonResult{
			OK:       false,
			Script:   safeRelName,
			Stdout:   stdoutStr,
			Stderr:   stderrStr,
			Duration: duration,
			Error:    fmt.Sprintf("python script timed out after %s", timeout),
		}
	}

	if runErr != nil {
		return PythonResult{
			OK:       false,
			Script:   safeRelName,
			Stdout:   stdoutStr,
			Stderr:   stderrStr,
			Duration: duration,
			Error:    runErr.Error(),
		}
	}

	return PythonResult{
		OK:       true,
		Script:   safeRelName,
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
		Duration: duration,
	}
}