param(
    [ValidateSet("list", "status", "up", "down")]
    [string]$Action = "list",

    [string]$Stack,

    [switch]$Force,

    [int]$MaxDepth = 3,

    [string[]]$ExtraRoots = @("C:\SYSTEM99", "C:\mcp"),

    [string[]]$ExcludeTopLevel = @(
        "Windows",
        "Program Files",
        "Program Files (x86)",
        "ProgramData",
        "Users",
        "PerfLogs",
        "Recovery",
        "`$Recycle.Bin",
        "System Volume Information",
        "Boot",
        "MSOCache",
        "Config.Msi",
        "Intel",
        "AMD",
        "Drivers",
        "Documents and Settings",
        "inetpub",
        "M1130MFP_M1210MFP_Full_Solution",
        "MSI",
        "Riot Games",
        "Shotcut",
        "Wemade",
        "`$AV_NLL"
    )
)

$ErrorActionPreference = "Stop"

$ComposeNames = @(
    "docker-compose.yml",
    "docker-compose.yaml",
    "compose.yml",
    "compose.yaml"
)

function Get-ComposeServicesAndPorts {
    param([string]$ComposePath)

    $services = @()
    $declaredPorts = @()
    $hostPorts = @()
    $currentService = $null
    $inServices = $false
    $inPorts = $false
    $servicesIndent = -1
    $serviceIndent = -1
    $portsIndent = -1

    foreach ($line in (Get-Content -LiteralPath $ComposePath -ErrorAction SilentlyContinue)) {
        if ($line -match '^\s*(#|$)') { continue }

        $leadingSpaces = 0
        if ($line -match '^(\s*)') {
            $leadingSpaces = $matches[1].Length
        }

        $trimmed = $line.Trim()

        if ($trimmed -eq "services:") {
            $inServices = $true
            $servicesIndent = $leadingSpaces
            $currentService = $null
            $inPorts = $false
            continue
        }

        if ($inServices -and $leadingSpaces -le $servicesIndent -and $trimmed -notmatch '^-') {
            $inServices = $false
            $currentService = $null
            $inPorts = $false
        }

        if ($inServices) {
            if ($serviceIndent -eq -1 -and $leadingSpaces -gt $servicesIndent -and $trimmed -match '^([a-zA-Z0-9_-]+):') {
                $serviceIndent = $leadingSpaces
                $currentService = $matches[1]
                $services += $currentService
                $inPorts = $false
                continue
            }
            elseif ($serviceIndent -ne -1 -and $leadingSpaces -eq $serviceIndent -and $trimmed -match '^([a-zA-Z0-9_-]+):') {
                $currentService = $matches[1]
                $services += $currentService
                $inPorts = $false
                continue
            }

            if ($currentService -and $trimmed -eq "ports:") {
                $inPorts = $true
                $portsIndent = $leadingSpaces
                continue
            }

            if ($inPorts) {
                if ($leadingSpaces -le $portsIndent -and $trimmed -notmatch '^-') {
                    $inPorts = $false
                }
                elseif ($trimmed -match '^-\s*"?([^"#]+)"?') {
                    $portMapping = $matches[1].Trim().Trim("'").Trim('"')
                    if ($portMapping) {
                        $declaredPorts += $portMapping
                        # Extrai porta de host quando estática (ex.: "80:80", "127.0.0.1:8080:80", "27015:27015/udp")
                        $cleanMapping = ($portMapping -split '/')[0]
                        $parts = $cleanMapping -split ':'
                        if ($parts.Count -ge 2) {
                            $candidateHostPort = $parts[$parts.Count - 2]
                            if ($candidateHostPort -match '^\d+$') {
                                $hostPorts += [int]$candidateHostPort
                            }
                        }
                    }
                }
            }
        }
    }

    [PSCustomObject]@{
        services       = @($services | Select-Object -Unique)
        declared_ports = @($declaredPorts | Select-Object -Unique)
        host_ports     = @($hostPorts | Select-Object -Unique)
    }
}

function Get-AllContainers {
    $list = @()
    $raw = docker ps -a --format "{{json .}}" 2>$null
    if ($raw) {
        foreach ($line in $raw) {
            if (-not [string]::IsNullOrWhiteSpace($line)) {
                $obj = $line | ConvertFrom-Json
                $composeProject = $null
                if ($obj.Labels -match '(?:^|,)com\.docker\.compose\.project=([^,]+)') {
                    $composeProject = $matches[1].ToLower()
                }
                $list += [PSCustomObject]@{
                    name            = $obj.Names
                    state           = $obj.State
                    status          = $obj.Status
                    ports           = $obj.Ports
                    compose_project = $composeProject
                }
            }
        }
    }
    return @($list)
}

function Get-StackContainers {
    param(
        [string]$StackName,
        [array]$Containers
    )

    $normalized = $StackName.ToLower()
    [object[]]$matched = @()

    foreach ($c in $Containers) {
        if (-not [string]::IsNullOrWhiteSpace($c.compose_project)) {
            if ($c.compose_project -eq $normalized) {
                $matched += [PSCustomObject]@{
                    name   = $c.name
                    state  = $c.state
                    status = $c.status
                    ports  = $c.ports
                }
            }
        }
        else {
            if ($c.name -like "$normalized*") {
                $matched += [PSCustomObject]@{
                    name   = $c.name
                    state  = $c.state
                    status = $c.status
                    ports  = $c.ports
                }
            }
        }
    }

    return ,$matched
}

function Get-PortConflicts {
    param([int[]]$HostPorts)

    if (-not $HostPorts -or $HostPorts.Count -eq 0) {
        return @()
    }

    $listening = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty LocalPort -Unique

    $conflicts = @()
    foreach ($p in $HostPorts) {
        if ($listening -contains $p) {
            $conflicts += $p
        }
    }
    return @($conflicts | Select-Object -Unique)
}

# 1. Raizes de C:\ filtradas + raizes extras
$DriveRoots = @()
if (Test-Path -LiteralPath "C:\") {
    $DriveRoots = Get-ChildItem -LiteralPath "C:\" -Directory -Force -ErrorAction SilentlyContinue |
        Where-Object {
            $dirName = $_.Name
            $matched = $false
            foreach ($ex in $ExcludeTopLevel) {
                if ($dirName -ieq $ex -or $dirName -like "$ex*") {
                    $matched = $true
                    break
                }
            }
            -not $matched
        } |
        Select-Object -ExpandProperty FullName
}

$SearchRoots = @($ExtraRoots + $DriveRoots) |
    Where-Object { $_ -and (Test-Path -LiteralPath $_) } |
    ForEach-Object { $_.TrimEnd("\") } |
    Sort-Object -Unique

# 2. Busca arquivos compose
$ComposeFiles = foreach ($root in $SearchRoots) {
    Get-ChildItem -LiteralPath $root -Recurse -Depth $MaxDepth -File -ErrorAction SilentlyContinue |
        Where-Object {
            $ComposeNames -contains $_.Name -and
            $_.FullName -notmatch "\\(node_modules|\.git|venv|\.venv|__pycache__)\\"
        }
}

# 3. Testa se o daemon Docker esta acessivel
$DaemonRunning = $false
$Containers = @()
docker info >$null 2>&1
if ($LASTEXITCODE -eq 0) {
    $DaemonRunning = $true
    $Containers = Get-AllContainers
}

# 4. Monta inventario das stacks
$Stacks = foreach ($file in ($ComposeFiles | Sort-Object FullName -Unique)) {
    $stackDir  = $file.Directory.FullName
    $stackName = $file.Directory.Name.ToLower()
    $parsed    = Get-ComposeServicesAndPorts -ComposePath $file.FullName

    $matchedContainers = @()
    if ($DaemonRunning) {
        $matchedContainers = Get-StackContainers -StackName $stackName -Containers $Containers
    }

    [PSCustomObject]@{
        stack              = $stackName
        compose_file       = $file.FullName
        stack_dir          = $stackDir
        services           = $parsed.services
        declared_ports     = $parsed.declared_ports
        host_ports         = $parsed.host_ports
        total_containers   = $matchedContainers.Count
        running_containers = @($matchedContainers | Where-Object { $_.state -eq "running" }).Count
        containers         = $matchedContainers
    }
}

# 5. Acoes unitarias (up / down / status)
if ($Action -in @("up", "down", "status")) {
    if ([string]::IsNullOrWhiteSpace($Stack)) {
        throw "Informe -Stack para a acao '$Action'."
    }

    $targetStack = $Stacks | Where-Object { $_.stack -ieq $Stack } | Select-Object -First 1
    if (-not $targetStack) {
        throw "Stack '$Stack' nao encontrada."
    }

    if (-not $DaemonRunning) {
        throw "Docker daemon nao esta rodando no momento. Inicie o Docker Desktop para executar '$Action' na stack '$Stack'."
    }

    switch ($Action) {
        "up" {
            # Evita subir se ja houver containers rodando nessa stack (a menos que -Force seja passado)
            if ($targetStack.running_containers -gt 0 -and -not $Force) {
                [PSCustomObject]@{
                    ok                 = $false
                    action             = "up"
                    dockerDaemonActive = $true
                    stack              = $targetStack.stack
                    reason             = "Stack ja possui containers ativos. Use -Force para reaplicar."
                    running_containers = $targetStack.running_containers
                    containers         = $targetStack.containers
                } | ConvertTo-Json -Depth 6
                exit 1
            }

            $portConflicts = Get-PortConflicts -HostPorts $targetStack.host_ports
            if ($portConflicts.Count -gt 0 -and -not $Force) {
                [PSCustomObject]@{
                    ok                 = $false
                    action             = "up"
                    dockerDaemonActive = $true
                    stack              = $targetStack.stack
                    reason             = "Conflito de portas TCP em escuta no host. Use -Force para forcar."
                    portConflicts      = $portConflicts
                    host_ports         = $targetStack.host_ports
                } | ConvertTo-Json -Depth 6
                exit 1
            }

            docker compose -f $targetStack.compose_file up -d | Out-Null
            if ($LASTEXITCODE -ne 0) {
                throw "Falha ao executar 'docker compose up -d' para a stack '$($targetStack.stack)'."
            }
        }
        "down" {
            docker compose -f $targetStack.compose_file down | Out-Null
            if ($LASTEXITCODE -ne 0) {
                throw "Falha ao executar 'docker compose down' para a stack '$($targetStack.stack)'."
            }
        }
        "status" {
            # Apenas retorna o estado atual
        }
    }

    $ContainersAfter = Get-AllContainers
    $afterMatched = Get-StackContainers -StackName $targetStack.stack -Containers $ContainersAfter

    [PSCustomObject]@{
        ok                 = $true
        action             = $Action
        dockerDaemonActive = $true
        stack              = $targetStack.stack
        compose_file       = $targetStack.compose_file
        services           = $targetStack.services
        declared_ports     = $targetStack.declared_ports
        host_ports         = $targetStack.host_ports
        total_containers   = $afterMatched.Count
        running_containers = @($afterMatched | Where-Object { $_.state -eq "running" }).Count
        containers         = $afterMatched
    } | ConvertTo-Json -Depth 6
    exit 0
}

[PSCustomObject]@{
    ok                 = $true
    action             = "list"
    dockerDaemonActive = $DaemonRunning
    scannedRoots       = $SearchRoots
    stackCount         = @($Stacks).Count
    stacks             = @($Stacks)
} | ConvertTo-Json -Depth 6