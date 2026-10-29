@echo off
cd /d "%~dp0"
echo Starting Terrain Studio desktop application...
start "" "%~dp0Terrain Studio Area Filter.exe"
if errorlevel 1 (
  echo.
  echo Terrain Studio could not start. See the error above.
  pause
)
