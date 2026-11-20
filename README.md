# 🛡️ GO-MCP

> **Status do Projeto:** 🚧 *Em desenvolvimento ativo (MVP funcional validado)*

Servidor **MCP (Model Context Protocol)** em **Go** projetado para atuar como *gateway* controlado entre agentes de IA e operações de infraestrutura local, containers Docker e automações PowerShell.

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![MCP SDK](https://img.shields.io/badge/MCP-Official%20Go%20SDK-00ff66?style=flat)](https://github.com/modelcontextprotocol/go-sdk)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Docker-0078D6?style=flat&logo=windows)](https://www.docker.com/)

---

## 🎯 Objetivo

Evitar conflitos de portas antes e durante o deploy de stacks **Docker Compose** e permitir que modelos de IA executem diagnósticos e rotinas operacionais sem acesso direto irrestrito ao sistema operacional.

---

## ✅ O que já está implementado e validado

- **Servidor MCP via `stdio` (`cmd/mcp-server/main.go`):** inicialização com o SDK oficial em Go (`StdioTransport`).
- **Tool `scan_ports` (`internal/ops/ports.go`):** varredura concorrente de portas TCP ativas cruzando listeners do Windows (`Get-NetTCPConnection` / `PID` / `ProcessName`) com bindings de containers (`docker ps`).
- **Tool `run_ps1` (`internal/ops/ps1.go`):** execução segura de scripts `.ps1` restrita ao diretório `./scripts`, com validação contra *path traversal*, `context.WithTimeout` e limite de buffer de saída (64 KB).
- **Script de Diagnóstico (`scripts/check_docker.ps1`):** verificação do status do daemon Docker retornando JSON estruturado.
- **Testes Ponta a Ponta (`internal/mcp/server_test.go`):** suite `TestToolsEndToEnd` validada via `go test -v ./...`, testando conexão MCP em memória, listagem e execução das duas tools.

---

## 🏗️ Arquitetura Atual

```text
Cliente MCP / LLM
       │ JSON-RPC 2.0 (stdio)
       ▼
go-mcp-infra (Go)  ──► Validação, Timeout & Isolamento de Diretório
  ├── scan_ports   ──► Listeners TCP (Windows) + Bindings (Docker)
  └── run_ps1      ──► Execução controlada de scripts em ./scripts