# GO-MCP Infra

> **Estado:** MVP em desenvolvimento. As funcionalidades descritas como “em evolução” ou “planeadas” ainda podem não estar integradas no servidor MCP.

Servidor MCP em Go para disponibilizar ferramentas controladas de diagnóstico e operações de infraestrutura, com foco em stacks Docker Compose num host Windows. O projeto usa um bridge HTTP no host para permitir que o servidor MCP — incluindo quando executado num container — invoque capacidades locais de forma explícita.

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![MCP](https://img.shields.io/badge/Protocol-MCP-4B6F44?style=flat)](https://modelcontextprotocol.io/)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Docker-0078D6?style=flat&logo=windows)](https://www.docker.com/)

## Objetivo

Permitir que um cliente/agente compatível com MCP consulte o estado de stacks Docker e, quando explicitamente solicitado, execute operações predefinidas no host Windows. A arquitetura separa o cliente MCP, o servidor Go, o bridge local e os scripts de automação.

O MCP Inspector é uma ferramenta de desenvolvimento para ligar-se ao servidor, inspecionar as ferramentas e testar chamadas. Não é o servidor MCP nem o agente de IA. Um cliente como Cursor pode atuar como interface/agente; o modelo utilizado depende da configuração desse cliente.

## Arquitetura

De acordo com a estrutura atual documentada do projeto, os componentes são:

1. **Servidor MCP em Go** (`cmd/mcp-server`): disponibiliza ferramentas MCP e encaminha pedidos ao bridge.
2. **MCP Inspector em container** (`debug/inspector/docker-compose.yml`): interface de inspeção e teste durante o desenvolvimento. O mapeamento documentado usa as portas `6274` (interface) e `6277` (proxy).
3. **Bridge HTTP para Windows** (`cmd/docker-bridge`): executável `docker-bridge.exe` no host, documentado a escutar na porta `8787` e a expor o endpoint `/docker`.
4. **Scripts locais** (`scripts/automation/` e `scripts/python/`): implementam operações específicas em PowerShell e Python.
5. **Docker Engine**: fornece o estado e as operações das stacks Compose no host.

```text
Cliente/agente MCP (por exemplo, Cursor)
                 │
                 │ MCP / JSON-RPC
                 ▼
        Servidor MCP Go (container)
                 │
                 │ HTTP para DOCKER_BRIDGE_URL
                 ▼
      docker-bridge.exe (Windows host)
          ├── scripts PowerShell
          ├── scripts Python
          └── Docker Engine / Docker Compose

MCP Inspector ── usado em desenvolvimento para testar o servidor MCP
```

O endereço `DOCKER_BRIDGE_URL` depende de onde o servidor está a correr. Em Docker Desktop, a configuração documentada pode usar `http://host.docker.internal:8787/docker`; confirma o valor no ambiente real antes de executar.

## Funcionalidades

### Docker Compose — disponível no fluxo atual

O script `scripts/automation/ListenDocker.ps1` documentado pelo projeto suporta as ações `list`, `status`, `up` e `down`. O inventário procura ficheiros Compose, recolhe serviços e portas declaradas e tenta associar containers às stacks. As ações de alteração devem ser usadas de forma explícita e apenas em stacks autorizadas.

### Health check Python — primeira melhoria

O script `scripts/python/health_check.py` foi evoluído para devolver JSON estruturado e verificar condições básicas do processo Python:

- acesso ao diretório de trabalho atual;
- capacidade de criar um ficheiro temporário;
- espaço livre no disco, com limite configurável;
- metadados do ambiente, como hostname, sistema operativo e versão do Python;
- estado agregado (`healthy`, `degraded` ou `unhealthy`) e resumo das verificações.

Esta verificação avalia o ambiente onde o processo Python corre. Não representa, por si só, um health check dos containers, das aplicações ou do Docker daemon.

Exemplo de execução:

```powershell
python scripts/python/health_check.py --tag local-check
python scripts/python/health_check.py --tag local-check --min-free-mb 1024
```

A saída JSON é enviada para `stdout`, para facilitar o consumo pelo bridge/MCP. O código de saída é zero para resultados sem erro; avisos de espaço baixo produzem estado `degraded` sem falha de execução.

### Descoberta de rede com Nmap — em integração

Está preparada uma primeira versão modular de `nmap_scan.py`, ainda a integrar no repositório e no servidor MCP. O desenho separa:

- `discover`: consulta rotas privadas conhecidas pelo sistema e apresenta sugestões; não inicia scans;
- `scan`: recebe um IPv4, CIDR ou intervalo IPv4 explícito, requer confirmação de autorização e executa descoberta de hosts (`nmap -sn`), sem scan de portas.

A rede `192.168.200.0/24` é uma sugestão inicial para o ambiente do utilizador, não uma autorização automática. Também podem ser usados alvos como `/29` ou um endereço `/32`, respeitando o limite configurado por execução. Redes encontradas nas rotas não implicam que todas as VLANs da organização estejam visíveis ou autorizadas.

O Nmap tem de estar instalado e acessível no `PATH` do ambiente onde o script Python é executado. A descoberta de hosts pode não detetar equipamentos que estejam offline ou bloqueiem probes.

### TShark — planeado

A análise com TShark ainda não está implementada. A evolução prevista é uma captura ao vivo com interface selecionada, duração limitada e filtros explícitos, seguida de um resumo JSON de metadados — por exemplo, protocolos, pares de IP/porta, DNS visível e volumes aproximados.

A captura pode conter dados sensíveis. Deve limitar-se a interfaces e redes próprias ou autorizadas; payloads não devem ser incluídos no relatório por omissão. O acesso do container a interfaces do host não é automático, pelo que a localização do processo e as permissões precisam de ser definidas antes da integração.

## Segurança e limites

- Não exponhas o bridge HTTP à Internet. Restringe-o à interface/rede necessária e aplica controlos de acesso adequados ao ambiente.
- Mantém os scripts como operações predefinidas com argumentos validados; não transformes o bridge numa execução arbitrária de comandos.
- Separa ferramentas de leitura (`list`, `status`, descoberta) de operações que alteram o estado (`up`, `down`). O cliente/agente deve pedir confirmação antes de executar ações com efeitos.
- Em scans de rede, exige alvo explícito, confirmação de autorização, limites de escopo e timeout. A descoberta de uma rota não concede autorização para a analisar.
- Não recolhas nem devolvas segredos, variáveis de ambiente completas ou payloads de rede sem necessidade.
- Revê os ficheiros staged antes do commit para evitar publicar credenciais, configurações locais ou capturas de tráfego.

## Desenvolvimento e testes

O servidor Go pode ser validado com:

```powershell
go vet ./...
go test ./...
```

O MCP Inspector pode ser usado para confirmar que o servidor anuncia as ferramentas esperadas e que as chamadas devolvem respostas estruturadas. Testa primeiro ferramentas read-only e casos de erro antes de habilitar operações como `up` e `down`.

O script de publicação Git deve ser executado a partir da raiz do repositório. Antes de publicar, confirma o resultado de `git status` e revê o conteúdo staged; evita assumir que `git add .` só adicionará ficheiros pretendidos.

## Roadmap sugerido

1. Integrar e testar o health check Python pelo bridge e pelo MCP.
2. Uniformizar o formato JSON e os códigos de saída dos scripts.
3. Integrar a descoberta de rotas e hosts com Nmap, mantendo o escopo explícito.
4. Expor ferramentas MCP separadas para descoberta e scan autorizado.
5. Acrescentar consulta read-only de logs de containers.
6. Implementar captura limitada e análise offline/ao vivo com TShark, após definir interface e permissões.
7. Correlacionar portas declaradas no Compose, portas observadas e evidências disponíveis.
8. Gerar achados com evidência, gravidade, recomendação e incerteza — sem afirmar vulnerabilidades não verificadas.
9. Acrescentar testes automatizados com saídas simuladas; testes não devem exigir scans reais.
10. Documentar a instalação, configuração e uma demonstração ponta a ponta.

## Estrutura de diretórios esperada

```text
.
├── cmd/
│   ├── mcp-server/
│   └── docker-bridge/
├── debug/
│   └── inspector/
│       └── docker-compose.yml
└── scripts/
    ├── automation/
    │   ├── ListenDocker.ps1
    │   └── check_docker.ps1
    └── python/
        ├── health_check.py
        └── nmap_scan.py   # em integração
```

A estrutura acima resume os caminhos em implantação.

## Licença

Em andamento..
