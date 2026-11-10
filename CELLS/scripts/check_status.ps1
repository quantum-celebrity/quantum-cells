# Simple status check for CELLS.exe
Write-Host "=== CELLS Status Check ===" -ForegroundColor Cyan
Write-Host ""

# Check if process is running
$process = Get-Process -Name "CELLS" -ErrorAction SilentlyContinue
if ($process) {
    Write-Host "✅ CELLS.exe is RUNNING" -ForegroundColor Green
    Write-Host "   PID: $($process.Id)" -ForegroundColor Gray
    Write-Host "   Started: $($process.StartTime)" -ForegroundColor Gray
    Write-Host ""
    
    # Check dashboard
    Write-Host "Checking dashboard..." -ForegroundColor Cyan
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:8081/api/health" -TimeoutSec 2 -UseBasicParsing
        $health = $response.Content | ConvertFrom-Json
        
        Write-Host "✅ Dashboard is responding!" -ForegroundColor Green
        Write-Host ""
        Write-Host "Overall Status: $($health.overall_status)" -ForegroundColor $(if ($health.overall_status -eq "healthy") { "Green" } else { "Yellow" })
        Write-Host ""
        Write-Host "Component Status:" -ForegroundColor Cyan
        
        $health.components.PSObject.Properties | ForEach-Object {
            $name = $_.Name
            $status = $_.Value.status
            $message = $_.Value.message
            
            $color = if ($status -eq "healthy") { "Green" } elseif ($status -eq "degraded") { "Yellow" } else { "Red" }
            Write-Host "  $name : $status" -ForegroundColor $color
            if ($message) {
                Write-Host "    $message" -ForegroundColor Gray
            }
        }
        
        Write-Host ""
        Write-Host "Access dashboard at: http://localhost:8081" -ForegroundColor Cyan
    } catch {
        Write-Host "⚠️  Dashboard not responding yet" -ForegroundColor Yellow
        Write-Host "   Error: $($_.Exception.Message)" -ForegroundColor Gray
        Write-Host "   The application may still be starting up..." -ForegroundColor Gray
    }
} else {
    Write-Host "❌ CELLS.exe is NOT running" -ForegroundColor Red
    Write-Host ""
    Write-Host "To start it, run:" -ForegroundColor Yellow
    Write-Host "  .\start_CELLS.ps1" -ForegroundColor Gray
    Write-Host "  or" -ForegroundColor Gray
    Write-Host "  .\run.ps1" -ForegroundColor Gray
}
