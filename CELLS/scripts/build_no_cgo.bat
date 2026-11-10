@echo off
REM Build CELLS without CGO dependencies (for development/testing)
REM This builds a version without WASM, quantum crypto, or CUDA support

echo Building CELLS without CGO dependencies...
echo This version will have limited functionality but will run without external C libraries.

set CGO_ENABLED=0
go build -o CELLS.exe ./cmd/CELLS

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Build successful! Executable: CELLS.exe
    echo.
    echo Note: This build does not include:
    echo   - WASM module support
    echo   - Quantum-safe cryptography (liboqs)
    echo   - CUDA acceleration
    echo.
    echo The dashboard and core functionality will still work.
) else (
    echo.
    echo Build failed. Check errors above.
    exit /b %ERRORLEVEL%
)

