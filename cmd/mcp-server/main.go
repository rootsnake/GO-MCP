package main

import (
	"context"
	"flag"
	"log"
	"os"

	mcphandler "go-mcp/internal/mcp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	scriptsDir := flag.String("scripts-dir", "./scripts", "Diretório permitido contendo scripts .ps1")
	flag.Parse()

	if _, err := os.Stat(*scriptsDir); os.IsNotExist(err) {
		_ = os.MkdirAll(*scriptsDir, 0o755)
	}

	server := mcphandler.NewServer(*scriptsDir)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("erro ao executar servidor MCP: %v", err)
	}
}