@echo off
cd /d "%~dp0"
set "terrainExecutable=%~dp0Terrain-Studio.exe"
if not exist "%terrainExecutable%" set "terrainExecutable=%~dp0dist\Terrain-Studio.exe"
if not exist "%terrainExecutable%" (
  echo Terrain Studio has not been built yet.
  echo Install Go 1.24 or newer, then run Build-Terrain-Studio.ps1 in PowerShell.
  pause
  exit /b 1
)
echo Starting Terrain Studio desktop application...
start "" "%terrainExecutable%"
if errorlevel 1 (
  echo.
  echo Terrain Studio could not start. See the error above.
  pause
)
