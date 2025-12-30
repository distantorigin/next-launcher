@echo off
REM Build script for Miriani-Next Updater
REM This ensures personal paths are not embedded in the distributed binary

echo ================================================
echo    Building Miriani-Next Updater
echo ================================================
echo.

REM Version can be passed as argument or defaults to "dev"
set VERSION=%1
if "%VERSION%"=="" set VERSION=dev

REM Clean previous builds
if exist updater.exe (
    echo Removing old binary...
    del updater.exe
)

echo Building version: %VERSION%
echo.
echo Build flags:
echo   -trimpath: Remove file system paths
echo   -s -w: Strip debug information
echo   -X main.version: Inject version string
echo.

REM Build with trimpath, stripped debug info, and version injection
go build -trimpath -ldflags="-s -w -X main.version=%VERSION%" -o updater.exe

if errorlevel 1 (
    echo.
    echo ERROR: Build failed!
    pause
    exit /b 1
)

echo.
echo ================================================
echo    Build Complete!
echo ================================================
echo.
echo Binary: updater.exe
echo Version: %VERSION%
echo.

REM Show file size
for %%A in (updater.exe) do echo Size: %%~zA bytes

echo.
echo The binary is ready for distribution.
echo Personal file paths have been removed.
echo.
pause
