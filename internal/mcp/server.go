package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"go-mcp/internal/ops"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ScanPortsInput struct {
	Host      string `json:"host,omitempty" jsonschema:"Host alvo para checar portas (padrão: 127.0.0.1)"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"Timeout por porta em ms (padrão: 300)"`
}

type RunPS1Input struct {
	Script     string   `json:"script" jsonschema:"Nome do arquivo .ps1 na pasta scripts (ex: check_docker.ps1)"`
	Args       []string `json:"args,omitempty" jsonschema:"Argumentos opcionais passados ao script .ps1"`
	TimeoutSec int      `json:"timeout_sec,omitempty" jsonschema:"Timeout de execução em segundos (padrão: 20)"`
}

type RunPythonInput struct {
	Script     string   `json:"script" jsonschema:"Nome do arquivo .py na pasta scripts (ex: health_check.py)"`
	Args       []string `json:"args,omitempty" jsonschema:"Argumentos opcionais passados ao script .py"`
	TimeoutSec int      `json:"timeout_sec,omitempty" jsonschema:"Timeout de execução em segundos (padrão: 20)"`
}

type DockerStackInput struct {
	Action string `json:"action" jsonschema:"Ação na stack via bridge: list, status, up ou down"`
	Stack  string `json:"stack,omitempty" jsonschema:"Nome da stack (ex: live)"`
	Force  bool   `json:"force,omitempty" jsonschema:"Força a operação quando aplicável"`
}

func NewServer(scriptsDir string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "go-mcp-infra",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "scan_ports",
		Description: "Lista portas TCP ativas, processos no Windows e bindings de containers Docker para evitar conflitos antes ou durante subir um Docker Compose.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ScanPortsInput) (*mcp.CallToolResult, any, error) {
		host := input.Host
		if host == "" {
			host = "127.0.0.1"
		}
		timeout := 300 * time.Millisecond
		if input.TimeoutMs > 0 {
			timeout = time.Duration(input.TimeoutMs) * time.Millisecond
		}

		reports := ops.ScanActivePorts(host, timeout)
		payload, err := json.MarshalIndent(reports, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao serializar relatório de portas: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_ps1",
		Description: "Executa com segurança um script PowerShell (.ps1) previamente localizado no diretório configurado de scripts.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RunPS1Input) (*mcp.CallToolResult, any, error) {
		timeout := 20 * time.Second
		if input.TimeoutSec > 0 {
			timeout = time.Duration(input.TimeoutSec) * time.Second
		}

		res := ops.RunPS1Script(scriptsDir, input.Script, input.Args, timeout)
		payload, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao serializar resultado do .ps1: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_python",
		Description: "Executa com segurança um script Python (.py) previamente localizado no diretório configurado de scripts e retorna o resultado estruturado.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RunPythonInput) (*mcp.CallToolResult, any, error) {
		timeout := 20 * time.Second
		if input.TimeoutSec > 0 {
			timeout = time.Duration(input.TimeoutSec) * time.Second
		}

		res := ops.RunPythonScript(scriptsDir, input.Script, input.Args, timeout)
		payload, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao serializar resultado do .py: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "docker_stack",
		Description: "Orquestra stacks Docker no host Windows via bridge HTTP (/docker) usando ações como list, status, up e down.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DockerStackInput) (*mcp.CallToolResult, any, error) {
		bridgeURL := os.Getenv("DOCKER_BRIDGE_URL")
		if bridgeURL == "" {
			bridgeURL = "http://host.docker.internal:8787/docker"
		}

		bodyBytes, err := json.Marshal(input)
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao serializar payload da stack: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, bridgeURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao criar requisição para o bridge: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao chamar bridge em %s: %w", bridgeURL, err)
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("falha ao ler resposta do bridge: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(respBytes)},
			},
		}, nil, nil
	})

	return server
}