@echo off
setlocal EnableExtensions EnableDelayedExpansion

REM Build and push Nakama Plus image to AWS ECR.
REM Default tag: current git short commit.

REM Always run from this script directory (build/)
cd /d "%~dp0"

set "DEBUG=false"

set "AWS_REGION=us-east-1"
set "AWS_ACCOUNT_ID=746669197317"
set "ECR_REPOSITORY=starhold-us/nakama-plus"
set "PLATFORM=linux/amd64"
set "NO_PUSH=false"
set "ALSO_TAG_LATEST=true"
set "RELEASE_TAG="
set "CREATE_REPO=false"

echo === build-aws-ecr.bat ===

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
if /i "%~1"=="--no-push" (
  set "NO_PUSH=true"
  shift
  goto parse_args
)
if /i "%~1"=="--also-tag-latest" (
  set "ALSO_TAG_LATEST=true"
  shift
  goto parse_args
)
if /i "%~1"=="--release-tag" (
  set "RELEASE_TAG=%~2"
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
echo Usage: build-aws-ecr.bat [options]
echo.
echo Options:
echo   --debug                Print verbose debug output
echo   --region REGION        AWS region (default: %AWS_REGION%)
echo   --account ACCOUNT_ID   AWS account id (default: %AWS_ACCOUNT_ID%)
echo   --repo REPOSITORY      ECR repository (default: %ECR_REPOSITORY%)
echo   --platform PLATFORM    Docker platform (default: %PLATFORM%)
echo   --no-push              Build only, do not push
echo   --also-tag-latest      Also push tag ":latest" (default: %ALSO_TAG_LATEST%)
echo   --release-tag TAG      Also push a specific release tag (e.g. v1.0.0)
echo   --create-repo          Create ECR repo if missing (default: %CREATE_REPO%)
echo   --help                 Show this help
echo.
echo Example:
echo   build-aws-ecr.bat --region us-east-1 --repo starhold-us/nakama-plus
exit /b 2

:start
if /i "%DEBUG%"=="true" (
  echo [DBG] script_dir=%~dp0
  echo [DBG] cwd=%cd%
  echo [DBG] AWS_REGION=%AWS_REGION%
  echo [DBG] AWS_ACCOUNT_ID=%AWS_ACCOUNT_ID%
  echo [DBG] ECR_REPOSITORY=%ECR_REPOSITORY%
  echo [DBG] PLATFORM=%PLATFORM%
  echo [DBG] NO_PUSH=%NO_PUSH%
  echo [DBG] ALSO_TAG_LATEST=%ALSO_TAG_LATEST%
  echo [DBG] CREATE_REPO=%CREATE_REPO%
)
REM ---- commit tag ----
set "COMMIT="
for /f "usebackq delims=" %%i in (`git rev-parse --short HEAD 2^>nul`) do set "COMMIT=%%i"
if not defined COMMIT (
  echo [WRN] Cannot determine git commit. Using 'dev' as fallback.
  set "COMMIT=dev"
)

set "TAG=%COMMIT%"
set "ECR_REGISTRY=%AWS_ACCOUNT_ID%.dkr.ecr.%AWS_REGION%.amazonaws.com"
set "IMAGE=%ECR_REGISTRY%/%ECR_REPOSITORY%"

echo ========================================
echo   Region   : %AWS_REGION%
echo   Account  : %AWS_ACCOUNT_ID%
echo   Repo     : %ECR_REPOSITORY%
echo   Image    : %IMAGE%
echo   Tag      : %TAG%
if defined RELEASE_TAG echo   Release  : %RELEASE_TAG%
echo   Platform : %PLATFORM%
echo   Push     : %NO_PUSH%
echo ========================================

REM ---- preflight ----
aws --version >nul 2>&1
if errorlevel 1 (
  echo [ERR] AWS CLI not found. Install AWS CLI v2 and ensure it is in PATH.
  exit /b 1
)

docker --version >nul 2>&1
if errorlevel 1 (
  echo [ERR] Docker not found. Install Docker and ensure it is in PATH.
  exit /b 1
)

docker buildx version >nul 2>&1
if errorlevel 1 (
  echo [ERR] Docker Buildx not available.
  exit /b 1
)

echo Logging in to ECR...
aws ecr get-login-password --region "%AWS_REGION%" | docker login --username AWS --password-stdin "%ECR_REGISTRY%"
if errorlevel 1 (
  echo [ERR] ECR login failed. Check AWS credentials and IAM permissions.
  exit /b 1
)

REM ---- create repo (optional) ----
if /i "%CREATE_REPO%"=="true" (
  echo Ensuring ECR repository exists: %ECR_REPOSITORY%
  aws ecr describe-repositories --region "%AWS_REGION%" --repository-names "%ECR_REPOSITORY%" >nul 2>&1
  if errorlevel 1 (
    aws ecr create-repository --region "%AWS_REGION%" --repository-name "%ECR_REPOSITORY%" --image-scanning-configuration scanOnPush=true >nul
    if errorlevel 1 (
      echo [ERR] Failed to create ECR repository: %ECR_REPOSITORY%
      exit /b 1
    )
  )

  echo Ensuring ECR repository exists: %ECR_REPOSITORY%-builder-base
  aws ecr describe-repositories --region "%AWS_REGION%" --repository-names "%ECR_REPOSITORY%-builder-base" >nul 2>&1
  if errorlevel 1 (
    aws ecr create-repository --region "%AWS_REGION%" --repository-name "%ECR_REPOSITORY%-builder-base" --image-scanning-configuration scanOnPush=true >nul
    if errorlevel 1 (
      echo [ERR] Failed to create ECR repository: %ECR_REPOSITORY%-builder-base
      exit /b 1
    )
  )

  echo Ensuring ECR repository exists: %ECR_REPOSITORY%-runtime-base
  aws ecr describe-repositories --region "%AWS_REGION%" --repository-names "%ECR_REPOSITORY%-runtime-base" >nul 2>&1
  if errorlevel 1 (
    aws ecr create-repository --region "%AWS_REGION%" --repository-name "%ECR_REPOSITORY%-runtime-base" --image-scanning-configuration scanOnPush=true >nul
    if errorlevel 1 (
      echo [ERR] Failed to create ECR repository: %ECR_REPOSITORY%-runtime-base
      exit /b 1
    )
  )
)

if not exist "C:\tmp\.buildx-cache" mkdir "C:\tmp\.buildx-cache"
if not exist "C:\tmp\.buildx-cache-new" mkdir "C:\tmp\.buildx-cache-new"

REM Cleanup lock files from previous failed builds
if exist "C:\tmp\.buildx-cache\index.json.lock" del /f /q "C:\tmp\.buildx-cache\index.json.lock"
if exist "C:\tmp\.buildx-cache-new\index.json.lock" del /f /q "C:\tmp\.buildx-cache-new\index.json.lock"

set "BUILDER_BASE_IMAGE=%IMAGE%-builder-base:bookworm-go1.25"
set "RUNTIME_BASE_IMAGE=%IMAGE%-runtime-base:bookworm"

echo.
echo Building builder base image: %BUILDER_BASE_IMAGE%
docker buildx build .. --platform "%PLATFORM%" --file ./Dockerfile.builder-base -t "%BUILDER_BASE_IMAGE%" --push
if errorlevel 1 (
  echo [ERR] Builder base image build failed.
  exit /b 1
)

echo.
echo Building runtime base image: %RUNTIME_BASE_IMAGE%
docker buildx build .. --platform "%PLATFORM%" --file ./Dockerfile.runtime-base -t "%RUNTIME_BASE_IMAGE%" --push
if errorlevel 1 (
  echo [ERR] Runtime base image build failed.
  exit /b 1
)

REM ---- build & push ----
set "BUILD_ARGS=buildx build .. --platform "%PLATFORM%" --file ./Dockerfile --build-arg COMMIT="%COMMIT%" --build-arg VERSION="%TAG%" --build-arg BUILDER_BASE_IMAGE="%BUILDER_BASE_IMAGE%" --build-arg RUNTIME_BASE_IMAGE="%RUNTIME_BASE_IMAGE%" -t "%IMAGE%:%TAG%""
if /i "%ALSO_TAG_LATEST%"=="true" (
  set "BUILD_ARGS=%BUILD_ARGS% -t "%IMAGE%:latest""
)
if defined RELEASE_TAG (
  set "BUILD_ARGS=%BUILD_ARGS% -t "%IMAGE%:%RELEASE_TAG%""
)
if /i "%NO_PUSH%"=="false" (
  set "BUILD_ARGS=%BUILD_ARGS% --push"
)

echo.
echo Building Nakama main image...
docker %BUILD_ARGS%
if errorlevel 1 (
  echo [ERR] Build or push failed.
  exit /b 1
)

if exist "C:\tmp\.buildx-cache-new" (
  rmdir /s /q "C:\tmp\.buildx-cache"
  if exist "C:\tmp\.buildx-cache-new" move "C:\tmp\.buildx-cache-new" "C:\tmp\.buildx-cache"
)

echo [OK] Done: %IMAGE%:%TAG%
exit /b 0

