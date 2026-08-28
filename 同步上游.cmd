@echo off
chcp 65001 >nul
cd /d "%~dp0"
call corepack pnpm sync:upstream %*
set "SYNC_EXIT_CODE=%ERRORLEVEL%"
echo.
pause
exit /b %SYNC_EXIT_CODE%
