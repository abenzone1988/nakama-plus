@echo off
setlocal EnableExtensions EnableDelayedExpansion

cd /d "%~dp0"

set "DEBUG=false"
set "AWS_REGION=us-east-1"
set "AWS_ACCOUNT_ID=746669197317"
set "ECR_REPOSITORY=starhold-us/nakama-plus"
set "MAX_ITEMS=30"
set "DELETE_SOURCE=false"
set "DELETE_IMAGE=false"
set "DELETE_FORCE=false"
set "SELECT_CHOICE="

echo === tag-ecr-image.bat ===

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
if /i "%~1"=="--max" (
  set "MAX_ITEMS=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--delete-source" (
  set "DELETE_SOURCE=true"
  shift
  goto parse_args
)
if /i "%~1"=="--delete-image" (
  set "DELETE_IMAGE=true"
  shift
  goto parse_args
)
if /i "%~1"=="--select" (
  set "SELECT_CHOICE=%~2"
  shift
  shift
  goto parse_args
)
if /i "%~1"=="--force" (
  set "DELETE_FORCE=true"
  shift
  goto parse_args
)
if /i "%~1"=="--help" goto help

echo [ERR] Unknown arg: %~1
goto help

:help
echo Usage: tag-ecr-image.bat [options]
echo.
echo Options:
echo   --debug                Print verbose debug output
echo   --region REGION        AWS region (default: %AWS_REGION%)
echo   --account ACCOUNT_ID   AWS account id (default: %AWS_ACCOUNT_ID%)
echo   --repo REPOSITORY      ECR repository (default: %ECR_REPOSITORY%)
echo   --max N                Show latest N tags (default: %MAX_ITEMS%)
echo   --delete-source        Delete the source tag after creating the new tag (default: %DELETE_SOURCE%)
echo   --delete-image         Delete an image by digest (removes all tags pointing to it)
echo   --select VALUE         Selection for --delete-image: index, tag, digest sha256, or last 12 chars (default: prompt)
echo   --force                For --delete-image: skip confirmation prompt
echo   --help                 Show this help
echo.
echo Example:
echo   tag-ecr-image.bat --repo starhold-us/nakama-plus --max 50
echo   tag-ecr-image.bat --delete-source
echo   tag-ecr-image.bat --delete-image
echo   tag-ecr-image.bat --delete-image --select 5 --force
exit /b 2

:start
set "ECR_REGISTRY=%AWS_ACCOUNT_ID%.dkr.ecr.%AWS_REGION%.amazonaws.com"
set "IMAGE=%ECR_REGISTRY%/%ECR_REPOSITORY%"

if /i "%DEBUG%"=="true" (
  echo [DBG] AWS_REGION=%AWS_REGION%
  echo [DBG] AWS_ACCOUNT_ID=%AWS_ACCOUNT_ID%
  echo [DBG] ECR_REPOSITORY=%ECR_REPOSITORY%
  echo [DBG] IMAGE=%IMAGE%
  echo [DBG] MAX_ITEMS=%MAX_ITEMS%
)

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

if /i "%DELETE_IMAGE%"=="true" goto delete_image

set "SELECTED_TAG="
set "NEW_TAG="

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference='Stop';" ^
  "$region='%AWS_REGION%'; $repo='%ECR_REPOSITORY%'; $max=[int]'%MAX_ITEMS%';" ^
  "$json = aws ecr describe-images --region $region --repository-name $repo --output json 2>$null;" ^
  "if ([string]::IsNullOrWhiteSpace($json)) { throw 'Failed to query ECR images.' }" ^
  "$data = $json | ConvertFrom-Json;" ^
  "$rows = foreach ($d in $data.imageDetails) {" ^
  "  if (-not $d.imageTags) { continue }" ^
  "  foreach ($t in $d.imageTags) {" ^
  "    [pscustomobject]@{ Tag=$t; PushedAt=[datetime]$d.imagePushedAt; Digest=$d.imageDigest }" ^
  "  }" ^
  "};" ^
  "$rows = $rows | Sort-Object PushedAt -Descending | Select-Object -First $max;" ^
  "if (-not $rows) { throw 'No tags found in repository.' }" ^
  "Write-Host ''; Write-Host 'Available tags:';" ^
  "$i=0; $rows | ForEach-Object { $shortDigest = if ($_.Digest) { $_.Digest.Substring([math]::Max(0,$_.Digest.Length-12)) } else { '' }; " ^
  "  '{0,3}  {1,-30}  {2}  {3}' -f $i, $_.Tag, $_.PushedAt.ToString('yyyy-MM-dd HH:mm:ss'), $shortDigest; $i++ } | Write-Host;" ^
  "Write-Host '';" ^
  "$choice = Read-Host 'Select source tag (index or tag name)';" ^
  "if ([string]::IsNullOrWhiteSpace($choice)) { throw 'Selection is required.' }" ^
  "if ($choice -match '^\d+$') { $idx=[int]$choice; if ($idx -lt 0 -or $idx -ge $rows.Count) { throw 'Index out of range.' }; $src=$rows[$idx].Tag } else { $src=$choice.Trim() }" ^
  "if ([string]::IsNullOrWhiteSpace($src)) { throw 'Source tag is required.' }" ^
  "  [System.IO.File]::WriteAllText((Join-Path $env:TEMP 'ecr_tag_src.txt'), $src, [System.Text.Encoding]::ASCII);" ^
  "$dst = Read-Host 'Enter new tag to create (e.g. 1.0.1)';" ^
  "if ([string]::IsNullOrWhiteSpace($dst)) { throw 'New tag is required.' }" ^
  "[System.IO.File]::WriteAllText((Join-Path $env:TEMP 'ecr_tag_dst.txt'), $dst, [System.Text.Encoding]::ASCII);"
if errorlevel 1 (
  echo [ERR] Tag selection failed.
  exit /b 1
)

set "TMP_SRC=%TEMP%\ecr_tag_src.txt"
set "TMP_DST=%TEMP%\ecr_tag_dst.txt"

if not exist "%TMP_SRC%" (
  echo [ERR] Missing selection file: %TMP_SRC%
  exit /b 1
)

set "SELECTED_TAG="
for /f "usebackq delims=" %%t in ("%TMP_SRC%") do if not defined SELECTED_TAG set "SELECTED_TAG=%%t"

del /q "%TMP_SRC%" >nul 2>&1

if "%SELECTED_TAG%"=="" (
  echo [ERR] Source tag is empty.
  exit /b 1
)

if not exist "%TMP_DST%" (
  echo [ERR] Missing selection file: %TMP_DST%
  exit /b 1
)

set "NEW_TAG="
for /f "usebackq delims=" %%n in ("%TMP_DST%") do if not defined NEW_TAG set "NEW_TAG=%%n"
del /q "%TMP_DST%" >nul 2>&1

if "%NEW_TAG%"=="" (
  echo [ERR] New tag is empty.
  exit /b 1
)

echo.
echo Creating tag: %IMAGE%:%NEW_TAG%  ^<=  %IMAGE%:%SELECTED_TAG%
aws ecr describe-images --region "%AWS_REGION%" --repository-name "%ECR_REPOSITORY%" --image-ids imageTag=%SELECTED_TAG% >nul 2>&1
if errorlevel 1 (
  echo [ERR] Source tag not found in ECR: %IMAGE%:%SELECTED_TAG%
  exit /b 1
)

docker buildx imagetools create -t "%IMAGE%:%NEW_TAG%" "%IMAGE%:%SELECTED_TAG%"
if errorlevel 1 (
  echo [ERR] Failed to create tag %NEW_TAG% from %SELECTED_TAG%.
  exit /b 1
)

if /i "%DELETE_SOURCE%"=="true" (
  if /i "%SELECTED_TAG%"=="%NEW_TAG%" (
    echo [WRN] Source tag equals new tag. Skip delete.
  ) else (
    echo.
    echo Deleting source tag: %IMAGE%:%SELECTED_TAG%
    aws ecr batch-delete-image --region "%AWS_REGION%" --repository-name "%ECR_REPOSITORY%" --image-ids imageTag=%SELECTED_TAG% >nul
    if errorlevel 1 (
      echo [ERR] Failed to delete source tag: %IMAGE%:%SELECTED_TAG%
      exit /b 1
    )
    echo [OK] Deleted source tag: %IMAGE%:%SELECTED_TAG%
  )
)

echo [OK] Created: %IMAGE%:%NEW_TAG%
exit /b 0

:delete_image
set "ECR_REGION=%AWS_REGION%"
set "ECR_REPO=%ECR_REPOSITORY%"
set "ECR_MAX=%MAX_ITEMS%"
set "ECR_SELECT=%SELECT_CHOICE%"
set "ECR_FORCE=%DELETE_FORCE%"

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference='Stop';" ^
  "$region=$env:ECR_REGION; $repo=$env:ECR_REPO; $max=[int]$env:ECR_MAX;" ^
  "$choice=$env:ECR_SELECT; $force=($env:ECR_FORCE -ieq 'true');" ^
  "$json = aws ecr describe-images --region $region --repository-name $repo --output json 2>$null;" ^
  "if ([string]::IsNullOrWhiteSpace($json)) { throw 'Failed to query ECR images.' }" ^
  "$data = $json | ConvertFrom-Json;" ^
  "$rows = foreach ($d in $data.imageDetails) {" ^
  "  if (-not $d.imageTags) { continue }" ^
  "  foreach ($t in $d.imageTags) { [pscustomobject]@{ Tag=$t; PushedAt=[datetime]$d.imagePushedAt; Digest=$d.imageDigest } }" ^
  "};" ^
  "$rows = $rows | Sort-Object PushedAt -Descending | Select-Object -First $max;" ^
  "if (-not $rows) { throw 'No tags found in repository.' }" ^
  "Write-Host ''; Write-Host 'Available tags:';" ^
  "$i=0; $rows | ForEach-Object { $shortDigest = if ($_.Digest) { $_.Digest.Substring([math]::Max(0,$_.Digest.Length-12)) } else { '' }; " ^
  "  '{0,3}  {1,-30}  {2}  {3}' -f $i, $_.Tag, $_.PushedAt.ToString('yyyy-MM-dd HH:mm:ss'), $shortDigest; $i++ } | Write-Host;" ^
  "Write-Host '';" ^
  "if ([string]::IsNullOrWhiteSpace($choice)) { $choice = Read-Host 'Select source (index, tag, digest sha256, or last 12 chars)' }" ^
  "if ([string]::IsNullOrWhiteSpace($choice)) { throw 'Selection is required.' }" ^
  "if ($choice -match '^\d+$') {" ^
  "  $idx=[int]$choice; if ($idx -lt 0 -or $idx -ge $rows.Count) { throw 'Index out of range.' }; $digest=$rows[$idx].Digest" ^
  "} else {" ^
  "  $c=$choice.Trim();" ^
  "  if ($c -match '^(?i)sha256:[0-9a-f]{64}$') { $digest=$c.ToLower() }" ^
  "  elseif ($c -match '^(?i)[0-9a-f]{64}$') { $digest=('sha256:' + $c.ToLower()) }" ^
  "  else {" ^
  "    $m = $rows | Where-Object { $_.Tag -eq $c } | Select-Object -First 1;" ^
  "    if ($m) { $digest=$m.Digest }" ^
  "    else { $m2 = $rows | Where-Object { $_.Digest -and ($_.Digest -ieq $c -or $_.Digest.EndsWith($c,[System.StringComparison]::OrdinalIgnoreCase)) } | Select-Object -First 1; $digest=$m2.Digest }" ^
  "  }" ^
  "}" ^
  "if ([string]::IsNullOrWhiteSpace($digest)) { throw 'Could not resolve an image digest from your selection.' }" ^
  "Write-Host ('Image digest: ' + $digest);" ^
  "Write-Host '[WRN] Deleting by digest will remove ALL tags pointing to this image.';" ^
  "if (-not $force) { $confirm = Read-Host 'Type DELETE to confirm'; if ($confirm -ine 'DELETE') { Write-Host '[WRN] Cancelled.'; exit 0 } }" ^
  "aws ecr batch-delete-image --region $region --repository-name $repo --image-ids imageDigest=$digest | Out-Null;" ^
  "if ($LASTEXITCODE -ne 0) { throw ('Failed to delete image: ' + $digest) }" ^
  "Write-Host ('[OK] Deleted image digest: ' + $digest);"
if errorlevel 1 (
  echo [ERR] Failed to delete image.
  exit /b 1
)
exit /b 0
