package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go-mcp/internal/ops"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ScanPortsInput struct {
	Host      string `json:"host,omitempty" jsonschema:"Host alvo para checar portas (padrão: 127.0.0.1)"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"Timeout por porta em ms (padrão: 300)"`
}

type RunPS1Input struct {
	Script    string   `json:"script" jsonschema:"Nome do arquivo .ps1 na pasta scripts (ex: check_docker.ps1)"`
	Args      []string `json:"args,omitempty" jsonschema:"Argumentos opcionais passados ao script .ps1"`
	TimeoutSec int     `json:"timeout_sec,omitempty" jsonschema:"Timeout de execução em segundos (padrão: 20)"`
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

	return server
}