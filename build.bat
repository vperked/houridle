@echo off
setlocal
cd /d "%~dp0"
set failed=
where npm >nul 2>nul || (echo Building needs Node.js for the control panel page: https://nodejs.org & set failed=1 & goto done)
pushd frontend
if not exist node_modules call npm ci --no-audit --no-fund || set failed=1
if not defined failed call npm run build || set failed=1
popd
if defined failed goto done
go build -trimpath -ldflags "-s -w" -o bin\houridle.exe ./backend/cmd/houridle && (echo Built bin\houridle.exe) || set failed=1
set GOOS=linux
go build -trimpath -ldflags "-s -w" -o bin\houridle ./backend/cmd/houridle && (echo Built bin\houridle for Linux) || set failed=1
:done
if not defined failed exit /b 0
echo Build failed, see the errors above.
echo %cmdcmdline% | find /i "%~nx0" >nul && pause
exit /b 1
