@echo off
:: Wrapper for deploy-windows.ps1
:: Double-click this file to run the deploy script with the window kept open.

title Nexa Exchange Deploy
cd /d "%~dp0.."

powershell.exe -NoExit -ExecutionPolicy Bypass -File "scripts\deploy-windows.ps1" %*

if errorlevel 1 (
    echo.
    echo [NEXA] Deploy failed. Check C:\nexa-exchange\deploy.log
    pause
    exit /b 1
)
