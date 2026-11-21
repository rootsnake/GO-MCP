package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type dockerRequest struct {
	Action string `json:"action"`
	Stack  string `json:"stack,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func buildPSArgs(scriptPath string, req dockerRequest) ([]string, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "list", "status", "up", "down":
	default:
		return nil, fmt.Errorf("acao invalida: %q (use list, status, up ou down)", req.Action)
	}

	args := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-Action", action,
	}

	stack := strings.TrimSpace(req.Stack)
	if action != "list" && stack != "" {
		args = append(args, "-Stack", stack)
	}

	if req.Force {
		args = append(args, "-Force")
	}

	return args, nil
}

func handleDocker(scriptPath string, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "use POST /docker",
			})
			return
		}

		defer r.Body.Close()
		var req dockerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "JSON invalido: " + err.Error(),
			})
			return
		}

		psArgs, err := buildPSArgs(scriptPath, req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}

		cmd := exec.Command("powershell.exe", psArgs...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		done := make(chan error, 1)
		go func() {
			done <- cmd.Run()
		}()

		select {
		case err := <-done:
			outTrimmed := strings.TrimSpace(stdout.String())
			errTrimmed := strings.TrimSpace(stderr.String())

			// Se o ListenDocker.ps1 ja retornou um JSON valido, repassa direto
			if json.Valid([]byte(outTrimmed)) {
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
				}
				_, _ = w.Write([]byte(outTrimmed))
				return
			}

			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":     false,
					"error":  err.Error(),
					"stderr": errTrimmed,
					"stdout": outTrimmed,
				})
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"stdout": outTrimmed,
				"stderr": errTrimmed,
			})

		case <-time.After(timeout):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			w.WriteHeader(http.StatusGatewayTimeout)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": errors.New("timeout ao executar ListenDocker.ps1"),
			})
		}
	}
}

func main() {
	addr := envOrDefault("LISTEN_ADDR", "127.0.0.1:8787")
	scriptPath := envOrDefault("SCRIPT_PATH", `C:\GO-MCP\scripts\automation\ListenDocker.ps1`)
	timeoutSec := 120 * time.Second

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "addr": addr})
	})
	mux.HandleFunc("/docker", handleDocker(scriptPath, timeoutSec))

	log.Printf("Bridge ouvindo em http://%s (script: %s)", addr, scriptPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}