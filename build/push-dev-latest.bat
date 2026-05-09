@echo off
setlocal EnableExtensions EnableDelayedExpansion

cd /d "%~dp0"

set "DEBUG=false"
set "AWS_REGION=us-east-1"
set "AWS_ACCOUNT_ID=746669197317"
set "ECR_REPOSITORY=starhold-us/nakama-plus"
set "PLATFORM=linux/amd64"
set "CREATE_REPO=false"

set "K8S_NAMESPACE=starhold"
set "K8S_CONTEXT="
set "DEV_YAML=%~dp0k8s\02-nakama-dev-statefulset.yaml"
set "DEV_STS_NAME=nakama-dev"

echo === push-dev-latest.bat ===

:parse_args
if "%~1"=="" goto start
if /i "%~1"=="--debug" (
  set "DEBUG=true"
  shift
  goto parse_args
)
if /i "%~1"=="--region" (
  set "AWS_REGION=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--account" (
  set "AWS_ACCOUNT_ID=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--repo" (
  set "ECR_REPOSITORY=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--platform" (
  set "PLATFORM=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--namespace" (
  set "K8S_NAMESPACE=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--context" (
  set "K8S_CONTEXT=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--create-repo" (
  set "CREATE_REPO=true"
  shift
  goto parse_args
)
if /i "%~1"=="--help" goto help

echo [ERR] Unknown arg: %~1
goto help

:help
echo Usage: push-dev-latest.bat [options]
echo.
echo Options:
echo   --debug                Print verbose debug output
echo   --region REGION        AWS region (default: %AWS_REGION%)
echo   --account ACCOUNT_ID   AWS account id (default: %AWS_ACCOUNT_ID%)
echo   --repo REPOSITORY      ECR repository (default: %ECR_REPOSITORY%)
echo   --platform PLATFORM    Docker platform (default: %PLATFORM%)
echo   --namespace NAMESPACE  Kubernetes namespace (default: %K8S_NAMESPACE%)
echo   --context CONTEXT      kubectl context (default: current)
echo   --create-repo          Create ECR repo if missing (default: %CREATE_REPO%)
echo   --help                 Show this help
echo.
echo Example:
echo   push-dev-latest.bat --context my-eks --namespace starhold
exit /b 2

:start
set "ECR_REGISTRY=%AWS_ACCOUNT_ID%.dkr.ecr.%AWS_REGION%.amazonaws.com"
set "IMAGE=%ECR_REGISTRY%/%ECR_REPOSITORY%"
set "TARGET_TAG=latest"

if /i "%DEBUG%"=="true" (
  echo [DBG] script_dir=%~dp0
  echo [DBG] AWS_REGION=%AWS_REGION%
  echo [DBG] AWS_ACCOUNT_ID=%AWS_ACCOUNT_ID%
  echo [DBG] ECR_REPOSITORY=%ECR_REPOSITORY%
  echo [DBG] PLATFORM=%PLATFORM%
  echo [DBG] CREATE_REPO=%CREATE_REPO%
  echo [DBG] DEV_YAML=%DEV_YAML%
  echo [DBG] IMAGE=%IMAGE%
  echo [DBG] TARGET_TAG=%TARGET_TAG%
  echo [DBG] K8S_NAMESPACE=%K8S_NAMESPACE%
  echo [DBG] K8S_CONTEXT=%K8S_CONTEXT%
)

if not exist "%DEV_YAML%" (
  echo [ERR] Dev StatefulSet YAML not found: %DEV_YAML%
  exit /b 1
)

echo.
echo [1/3] Building and pushing image (also tags :latest)...
set "ALSO_TAG_LATEST=true"
set "CREATE_REPO_ARG="
if /i "%CREATE_REPO%"=="true" set "CREATE_REPO_ARG=--create-repo"
call "%~dp0build-aws-ecr.bat" --region "%AWS_REGION%" --account "%AWS_ACCOUNT_ID%" --repo "%ECR_REPOSITORY%" --platform "%PLATFORM%" %CREATE_REPO_ARG%
if errorlevel 1 exit /b 1

echo.
echo [2/3] Updating dev YAML image to %IMAGE%:%TARGET_TAG% ...
copy /y "%DEV_YAML%" "%DEV_YAML%.bak" >nul
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$p = '%DEV_YAML%'; $img = '%IMAGE%'; $tag = '%TARGET_TAG%'; $c = Get-Content -Raw -LiteralPath $p; " ^
  "$c = [regex]::Replace($c, '(?m)^(\s*image:\s*)\S+/nakama-plus:[^\s]+', { param($m) $m.Groups[1].Value + $img + ':' + $tag }); " ^
  "Set-Content -LiteralPath $p -Value $c -Encoding utf8"
if errorlevel 1 (
  echo [ERR] Failed to update YAML: %DEV_YAML%
  exit /b 1
)

echo.
echo [3/3] Applying to Kubernetes...
kubectl version --client >nul 2>&1
if errorlevel 1 (
  echo [ERR] kubectl not found. Install kubectl and ensure it is in PATH.
  exit /b 1
)

set "KUBECTL=kubectl"
if defined K8S_CONTEXT set "KUBECTL=kubectl --context %K8S_CONTEXT%"

%KUBECTL% apply -n "%K8S_NAMESPACE%" -f "%DEV_YAML%"
if errorlevel 1 (
  echo [ERR] kubectl apply failed.
  exit /b 1
)

%KUBECTL% rollout status -n "%K8S_NAMESPACE%" statefulset/%DEV_STS_NAME%
if errorlevel 1 (
  echo [WRN] Rollout status returned non-zero. Check cluster events/logs.
)

echo [OK] Dev updated to %IMAGE%:%TARGET_TAG%
exit /b 0
