@echo off
REM Build script for Miriani-Next Updater
REM This ensures personal paths are not embedded in the distributed binary

echo ================================================
echo    Building Miriani-Next Updater
echo ================================================
echo.

REM Clean previous builds
if exist updater.exe (
    echo Removing old binary...
    del updater.exe
)

echo Building with secure flags...
echo   -trimpath: Remove file system paths
echo   -s -w: Strip debug information
echo   -H windowsgui: Windows GUI subsystem (no console popup)
echo.

REM Build with trimpath, stripped debug info, and Windows GUI subsystem
go build -trimpath -ldflags="-s -w" -o updater.exe

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
echo.

REM Show file size
for %%A in (updater.exe) do echo Size: %%~zA bytes

echo.
echo The binary is ready for distribution.
echo Personal file paths have been removed.
echo.
pause
