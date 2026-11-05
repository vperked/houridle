@echo off
rem Builds houridle.exe (Windows) and houridle (Linux) next to this file.
rem setlocal keeps GOOS=linux from leaking into the console that ran this.
setlocal
cd /d "%~dp0"
set failed=
go build -trimpath -ldflags "-s -w" -o houridle.exe ./src && (echo Built houridle.exe) || set failed=1
set GOOS=linux
go build -trimpath -ldflags "-s -w" -o houridle ./src && (echo Built houridle for Linux) || set failed=1
if not defined failed exit /b 0
echo Build failed, see the errors above.
rem Opened by double-click: keep the window open so the errors can be read.
echo %cmdcmdline% | find /i "%~nx0" >nul && pause
exit /b 1
