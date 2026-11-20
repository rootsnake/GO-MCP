package mcp

import (
"context"
"path/filepath"
"testing"

"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsEndToEnd(t *testing.T) {
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

scriptsDir, err := filepath.Abs(filepath.Join("..", "..", "scripts"))
if err != nil {
t.Fatalf("erro ao resolver scriptsDir: %v", err)
}

server := NewServer(scriptsDir)

t1, t2 := mcp.NewInMemoryTransports()

go func() {
_ = server.Run(ctx, t1)
}()

client := mcp.NewClient(&mcp.Implementation{
Name:    "test-client",
Version: "0.1.0",
}, nil)

session, err := client.Connect(ctx, t2, nil)
if err != nil {
t.Fatalf("erro ao conectar cliente MCP em memória: %v", err)
}
defer func() { _ = session.Close() }()

toolsRes, err := session.ListTools(ctx, nil)
if err != nil {
t.Fatalf("erro ao listar tools: %v", err)
}
if len(toolsRes.Tools) != 3 {
t.Fatalf("esperava 3 tools, veio %d", len(toolsRes.Tools))
}

portsOut, err := session.CallTool(ctx, &mcp.CallToolParams{
Name: "scan_ports",
Arguments: map[string]any{
"host": "127.0.0.1",
},
})
if err != nil {
t.Fatalf("erro ao chamar scan_ports: %v", err)
}
if len(portsOut.Content) == 0 {
t.Fatalf("scan_ports retornou conteúdo vazio")
}

ps1Out, err := session.CallTool(ctx, &mcp.CallToolParams{
Name: "run_ps1",
Arguments: map[string]any{
"script": "check_docker.ps1",
},
})
if err != nil {
t.Fatalf("erro ao chamar run_ps1: %v", err)
}
if len(ps1Out.Content) == 0 {
t.Fatalf("run_ps1 retornou conteúdo vazio")
}

pyOut, err := session.CallTool(ctx, &mcp.CallToolParams{
Name: "run_python",
Arguments: map[string]any{
"script": "health_check.py",
},
})
if err != nil {
t.Fatalf("erro ao chamar run_python: %v", err)
}
if len(pyOut.Content) == 0 {
t.Fatalf("run_python retornou conteúdo vazio")
}
}
