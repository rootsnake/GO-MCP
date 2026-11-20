# 🛡️ GO-MCP

> **Status do Projeto:** 🚧 *Em desenvolvimento ativo (MVP funcional e validado)*[cite: 5]

Servidor **MCP (Model Context Protocol)** em **Go** projetado para atuar como *gateway* controlado entre agentes de IA e operações de infraestrutura local, containers Docker e automações de scripts[cite: 5].

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![MCP SDK](https://img.shields.io/badge/MCP-Official%20Go%20SDK-00ff66?style=flat)](https://github.com/modelcontextprotocol/go-sdk)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Docker-0078D6?style=flat&logo=windows)](https://www.docker.com/)

---

## 🎯 Objetivo

Evitar conflitos de portas antes e durante o deploy de stacks **Docker Compose** e permitir que modelos de IA executem diagnósticos e rotinas operacionais sem acesso direto irrestrito ao sistema operacional[cite: 4, 5].

---

## ✅ O que já está implementado e validado

- **Servidor MCP via `stdio` (`cmd/mcp-server/main.go`):** inicialização com o SDK oficial em Go (`StdioTransport`) e suporte a `--scripts-dir`[cite: 5].
- **Tool `scan_ports` (`internal/ops/ports.go`):** varredura de portas TCP ativas e checagem de bindings/conflitos para apoiar subidas de Docker Compose; validada via MCP Inspector[cite: 4, 5].
- **Tool `run_python`:** execução controlada de scripts `.py` a partir do diretório de scripts configurado; validada no MCP Inspector.
- **Tool `run_ps1` (`internal/ops/ps1.go`):** execução controlada de scripts `.ps1` via PowerShell, com validação de caminho/diretório, timeout e limite de saída.
- **Scripts de Diagnóstico e Automação:**
  - `automation/check_docker.ps1`: verificação do status do ambiente/daemon Docker.
  - `python/health_check.py`: rotina de verificação/diagnóstico em Python[cite: 1].
- **MCP Inspector (`debug/inspector/docker-compose.yml`):** ambiente de depuração configurado nas portas `6274` e `6277`[cite: 4].
- **Testes Ponta a Ponta (`internal/mcp/server_test.go`):** suite `TestToolsEndToEnd` executada via `go test ./...` e integrada ao fluxo de deploy (`deploy.ps1`)[cite: 5].

---

## 🏗️ Arquitetura Atual

```text
Cliente MCP / LLM / MCP Inspector (:6274 / :6277)
       │ JSON-RPC 2.0 (stdio)
       ▼
go-mcp-server (Go)  ──► Validação, Timeout & Diretório de Scripts (--scripts-dir)
  ├── scan_ports    ──► Checagem de portas TCP e bindings Docker
  ├── run_python    ──► Execução de scripts Python (.py)
  └── run_ps1       ──► Execução de scripts PowerShell (.ps1)