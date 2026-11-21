# 🛡️ GO-MCP

> **Status do Projeto:** 🚧 *Em desenvolvimento ativo (MVP funcional e validado)*

Servidor **MCP (Model Context Protocol)** em **Go** projetado para atuar como *gateway* controlado entre agentes de IA, o **MCP Inspector** em container e operações de infraestrutura/stacks **Docker** no host Windows via bridge HTTP[cite: 1, 2, 4].

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![MCP SDK](https://img.shields.io/badge/MCP-Official%20Go%20SDK-00ff66?style=flat)](https://github.com/modelcontextprotocol/go-sdk)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Docker-0078D6?style=flat&logo=windows)](https://www.docker.com/)

---

## 🎯 Objetivo

Orquestrar diagnósticos de rede/portas, execução controlada de scripts e operações em stacks **Docker Compose** no host Windows a partir de um ambiente MCP isolado, evitando conflitos de portas e exposições diretas sem controle.

---

## 🏗️ Arquitetura e Estruturas

O sistema é dividido em três estruturas integradas:

1. **GO-MCP + MCP Inspector (Container / `stdio`):**
   - O **MCP Inspector** (`go-mcp-inspector`) sobe em container (`debug/inspector/docker-compose.yml`) nas portas `6274` (UI) e `6277` (proxy)[cite: 2, 4].
   - O servidor MCP (`go-mcp-infra` / `cmd/mcp-server`) é compilado para Linux (`GOOS=linux GOARCH=amd64`) e executado via `stdio` com `DOCKER_BRIDGE_URL` apontando para o bridge no host[cite: 1, 2, 3].
2. **Windows HTTP Bridge (`cmd/docker-bridge` / `docker-bridge.exe`):**
   - Roda no host Windows (ex.: `C:\AUTOM\docker-bridge.exe` ouvindo em `0.0.0.0:8787`) expondo `/docker` para orquestrar stacks e consultar o ambiente local.
3. **Docker Engine & Scripts (`ListenDocker.ps1` e `scripts/`):**
   - Executa verificações e ações em stacks Docker no host Windows por meio de `scripts/automation/ListenDocker.ps1`, `scripts/automation/check_docker.ps1` e `scripts/python/health_check.py`[cite: 1, 4].

```text
MCP Inspector (:6274 / :6277) / Cliente MCP
       │ stdio (JSON-RPC 2.0)
       ▼
mcp-server-linux (Go) ──► DOCKER_BRIDGE_URL ([http://host.docker.internal:8787/docker](http://host.docker.internal:8787/docker))
  ├── docker_stack    ──► Orquestra stacks via /docker (list, status, up, down)
  ├── scan_ports      ──► Varredura de portas TCP e conflitos/bindings
  ├── run_python      ──► Execução controlada de scripts Python (.py)
  └── run_ps1         ──► Execução controlada de scripts PowerShell (.ps1)
                                │
                                ▼
                   docker-bridge.exe (:8787) [Windows]
                                │
                                ▼
                   ListenDocker.ps1 & Docker Engine