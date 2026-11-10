# Check and fix port conflict for CELLS dashboard
Write-Host "=== Port Conflict Check ===" -ForegroundColor Cyan
Write-Host ""

$dashboardPort = 8081
$logViewerPort = 8080

# Check dashboard port
Write-Host "Checking port $dashboardPort (dashboard)..." -ForegroundColor Cyan
$dashboardConn = Get-NetTCPConnection -LocalPort $dashboardPort -ErrorAction SilentlyContinue
if ($dashboardConn) {
    $processId = $dashboardConn.OwningProcess | Select-Object -First 1
    $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
    Write-Host "⚠️  Port $dashboardPort is in use by:" -ForegroundColor Yellow
    Write-Host "   Process: $($process.ProcessName) (PID: $processId)" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Options:" -ForegroundColor Cyan
    Write-Host "  1. Stop the conflicting process:" -ForegroundColor Gray
    Write-Host "     Stop-Process -Id $processId" -ForegroundColor White
    Write-Host "  2. Change CELLS dashboard port in config" -ForegroundColor Gray
    Write-Host "  3. Use a different port when starting CELLS" -ForegroundColor Gray
} else {
    Write-Host "✅ Port $dashboardPort is available" -ForegroundColor Green
}

Write-Host ""

# Check log viewer port
Write-Host "Checking port $logViewerPort (log viewer)..." -ForegroundColor Cyan
$logViewerConn = Get-NetTCPConnection -LocalPort $logViewerPort -ErrorAction SilentlyContinue
if ($logViewerConn) {
    $processId = $logViewerConn.OwningProcess | Select-Object -First 1
    $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
    Write-Host "⚠️  Port $logViewerPort is in use by:" -ForegroundColor Yellow
    Write-Host "   Process: $($process.ProcessName) (PID: $processId)" -ForegroundColor Gray
} else {
    Write-Host "✅ Port $logViewerPort is available" -ForegroundColor Green
}

Write-Host ""
Write-Host "=== CELLS Process Status ===" -ForegroundColor Cyan
$CELLSProcess = Get-Process CELLS -ErrorAction SilentlyContinue
if ($CELLSProcess) {
    Write-Host "✅ CELLS.exe is running (PID: $($CELLSProcess.Id))" -ForegroundColor Green
    Write-Host "   Started: $($CELLSProcess.StartTime)" -ForegroundColor Gray
} else {
    Write-Host "❌ CELLS.exe is NOT running" -ForegroundColor Red
    Write-Host ""
    Write-Host "If port $dashboardPort is in use, CELLS may have failed to start the dashboard." -ForegroundColor Yellow
    Write-Host "Check the application output for errors." -ForegroundColor Gray
}

