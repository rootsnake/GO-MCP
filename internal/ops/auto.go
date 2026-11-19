package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxOutputBytes = 64 * 1024

type PS1Result struct {
	OK       bool   `json:"ok"`
	Script   string `json:"script"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

func RunPS1Script(scriptsDir, scriptName string, args []string, timeout time.Duration) PS1Result {
	start := time.Now()

	cleanName := filepath.Base(scriptName)
	if cleanName == "" || cleanName == "." || cleanName != scriptName || !strings.HasSuffix(strings.ToLower(cleanName), ".ps1") {
		return PS1Result{
			OK:       false,
			Script:   scriptName,
			Duration: time.Since(start).String(),
			Error:    "nome de script inválido: use apenas um arquivo .ps1 dentro da pasta de scripts",
		}
	}

	absDir, err := filepath.Abs(scriptsDir)
	if err != nil {
		return PS1Result{
			OK:       false,
			Script:   cleanName,
			Duration: time.Since(start).String(),
			Error:    "falha ao resolver diretório de scripts: " + err.Error(),
		}
	}

	fullPath := filepath.Join(absDir, cleanName)
	rel, err := filepath.Rel(absDir, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return PS1Result{
			OK:       false,
			Script:   cleanName,
			Duration: time.Since(start).String(),
			Error:    "caminho fora do diretório permitido",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := []string{
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", fullPath,
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "powershell.exe", cmdArgs...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	duration := time.Since(start).String()

	outStr := truncateOutput(stdoutBuf.String())
	errStr := truncateOutput(stderrBuf.String())

	if err != nil {
		msg := err.Error()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("timeout excedido após %s", timeout)
		}
		return PS1Result{
			OK:       false,
			Script:   cleanName,
			Stdout:   outStr,
			Stderr:   errStr,
			Duration: duration,
			Error:    msg,
		}
	}

	return PS1Result{
		OK:       true,
		Script:   cleanName,
		Stdout:   outStr,
		Stderr:   errStr,
		Duration: duration,
	}
}

func truncateOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "\n...[saída truncada]"
}