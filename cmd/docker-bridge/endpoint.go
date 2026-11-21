Set-Location C:\GO-MCP
New-Item -ItemType Directory -Force -Path .\cmd\docker-bridge | Out-Null
Copy-Item C:\AUTOM\main.go .\cmd\docker-bridge\endpoint.go