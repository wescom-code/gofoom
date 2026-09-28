@echo off
setlocal
set API_URL=http://127.0.0.1:9999

if "%1"=="" goto help
if "%1"=="status" goto status
if "%1"=="start" goto start
if "%1"=="stop" goto stop
if "%1"=="config" goto config
if "%1"=="autodetect" goto autodetect
if "%1"=="validate" goto validate
if "%1"=="shutdown" goto shutdown
if "%1"=="--help" goto help
if "%1"=="-h" goto help

:help
echo Gofoom CLI management script
echo.
echo Usage: foomcli [command] [options]
echo.
echo Commands:
echo   status      Check local proxy status (up/stopped)
echo   start       Start local proxy server
echo   stop        Stop local proxy server
echo   config      Display or update proxy configuration
echo               -f [file.json] : Load configuration from JSON file
echo   autodetect  Auto-detect system proxy settings
echo   validate    Validate proxy configuration via test URL
echo   shutdown    Shutdown Gofoom process
goto end

:status
curl -s %API_URL%/status
goto end

:start
curl -s -X POST %API_URL%/start
goto end

:stop
curl -s -X POST %API_URL%/stop
goto end

:config
if "%2"=="-f" (
    if "%3"=="" (
        echo Error: Missing JSON file path
        goto end
    )
    curl -s -X POST -H "Content-Type: application/json" --data-binary @"%3" %API_URL%/config
) else (
    curl -s %API_URL%/config
)
goto end

:autodetect
curl -s %API_URL%/autodetect
goto end

:validate
curl -s %API_URL%/validate
goto end

:shutdown
curl -s -X POST %API_URL%/shutdown
goto end

:end
endlocal
