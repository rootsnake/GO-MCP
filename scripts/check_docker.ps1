$dockerRunning = $false
try {
    docker info *> $null
    if ($LASTEXITCODE -eq 0) {
        $dockerRunning = $true
    }
} catch {
    $dockerRunning = $false
}

$result = [PSCustomObject]@{
    timestamp      = (Get-Date).ToString("o")
    computer_name  = $env:COMPUTERNAME
    docker_running = $dockerRunning
}

$result | ConvertTo-Json -Compress