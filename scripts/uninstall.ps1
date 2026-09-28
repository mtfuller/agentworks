param(
  [string]$Destination = $(if ($env:AGENTWORKS_INSTALL_DIR) { $env:AGENTWORKS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "AgentWorks\bin" })
)
$ErrorActionPreference = "Stop"
$target = Join-Path $Destination "agentworks.exe"
if (Test-Path -LiteralPath $target) {
  Remove-Item -LiteralPath $target -Force
  Write-Output "Removed $target"
} else {
  Write-Output "AgentWorks is not installed at $target"
}
Write-Output "Project definitions and local runtime data were left in place."
