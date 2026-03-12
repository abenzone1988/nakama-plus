@echo off


setlocal EnableExtensions EnableDelayedExpansion

REM Always run from this script directory (build/)
cd /d "%~dp0"

set "REGISTRY=192.168.102.224:30500"
set "IMAGE_NAME=nakama-plus"
set "FULL_IMAGE=%REGISTRY%/%IMAGE_NAME%"


if not "%~1"=="" (
  set "VERSION=%~1"
) else (
  set "TAG="
  for /f "usebackq delims=" %%i in (`git describe --tags --abbrev=0 2^>nul`) do set "TAG=%%i"
  if not defined TAG set "TAG=dev"
  set "VERSION=!TAG!"
  if /i "!VERSION:~0,1!"=="v" set "VERSION=!VERSION:~1!"
)

if not "%~2"=="" (
  set "PLATFORM=%~2"
) else (
  set "PLATFORM=linux/amd64"
)

REM ---- commit hash ----
set "COMMIT="
for /f "usebackq delims=" %%i in (`git rev-parse --short HEAD 2^>nul`) do set "COMMIT=%%i"
if not defined COMMIT set "COMMIT=unknown"

echo ========================================
echo   Registry : %REGISTRY%
echo   Image    : %FULL_IMAGE%
echo   Version  : %VERSION%
echo   Commit   : %COMMIT%
echo   Platform : %PLATFORM%
echo ========================================

REM NOTE: Avoid caret line continuations to prevent encoding/trailing-space issues.
docker buildx build .. --platform "%PLATFORM%" --file "Dockerfile.local" --build-arg COMMIT="%COMMIT%" --build-arg VERSION="%VERSION%" -t "%FULL_IMAGE%:%VERSION%" -t "%FULL_IMAGE%:latest" --push

if errorlevel 1 (
  echo.
  echo [ERR] buildx build or push failed
  exit /b 1
)

echo.
echo [OK] Push completed:
echo      %FULL_IMAGE%:%VERSION%
echo      %FULL_IMAGE%:latest
echo.
echo Verify:
echo   curl http://%REGISTRY%/v2/%IMAGE_NAME%/tags/list

