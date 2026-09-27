<#
.SYNOPSIS
  Trust (or stop trusting) the NeuroMesh dev CA for the current Windows user.

.DESCRIPTION
  `task certs:trust` adds certs/dev/ca.crt to CurrentUser\Root, so browsers,
  curl (Schannel) and Go clients accept https://localhost:8443 without
  --cacert. No elevation is needed; Windows shows one confirmation dialog.
  `task certs:untrust` removes it again. Re-run trust after `task certs:dev
  -- -force`, which mints a new CA.

  Only ever the throwaway CA from `task certs:dev`. On a cluster, cert-manager
  issues certificates and this script has no part in it.
#>
param(
  [switch]$Remove,
  [string]$CaPath = (Join-Path $PSScriptRoot "..\certs\dev\ca.crt")
)
$ErrorActionPreference = "Stop"
$subject = "CN=NeuroMesh dev CA, O=NeuroMesh dev"
$store = "Cert:\CurrentUser\Root"

$existing = Get-ChildItem $store | Where-Object { $_.Subject -eq $subject }

if ($Remove) {
  if (-not $existing) { Write-Host "No NeuroMesh dev CA is trusted."; exit 0 }
  $existing | ForEach-Object { Remove-Item (Join-Path $store $_.Thumbprint) }
  Write-Host "Removed $($existing.Count) NeuroMesh dev CA certificate(s)."
  exit 0
}

if (-not (Test-Path $CaPath)) { throw "$CaPath not found: run 'task certs:dev' first." }
$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2 (Resolve-Path $CaPath).Path

# A previous CA from an older `certs:dev -- -force` run is stale; replace it.
$existing | Where-Object { $_.Thumbprint -ne $cert.Thumbprint } | ForEach-Object {
  Remove-Item (Join-Path $store $_.Thumbprint)
  Write-Host "Removed stale dev CA $($_.Thumbprint)."
}
if ($existing | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }) {
  Write-Host "The current dev CA is already trusted."
  exit 0
}
Import-Certificate -FilePath (Resolve-Path $CaPath).Path -CertStoreLocation $store | Out-Null
Write-Host "Trusted $subject ($($cert.Thumbprint)) for the current user, until $($cert.NotAfter.ToString('yyyy-MM-dd'))."
