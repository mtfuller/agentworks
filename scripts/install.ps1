param(
  [string]$Source = (Join-Path $PSScriptRoot "agentworks.exe"),
  [string]$Destination = $(if ($env:AGENTWORKS_INSTALL_DIR) { $env:AGENTWORKS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "AgentWorks\bin" })
)
$ErrorActionPreference = "Stop"
if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) { throw "AgentWorks binary not found: $Source" }
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
$temporary = Join-Path $Destination (".agentworks-install-" + [guid]::NewGuid().ToString("N") + ".exe")
try {
  Copy-Item -LiteralPath $Source -Destination $temporary
  Move-Item -LiteralPath $temporary -Destination (Join-Path $Destination "agentworks.exe") -Force
} finally {
  Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
}
Write-Output "Installed AgentWorks to $(Join-Path $Destination 'agentworks.exe')"
if (($env:PATH -split ';') -notcontains $Destination) { Write-Output "Add $Destination to PATH to run agentworks." }
