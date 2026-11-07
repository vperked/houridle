@echo off
setlocal
cd /d "%~dp0"
set failed=
where npm >nul 2>nul || (echo Building needs Node.js for the control panel page: https://nodejs.org & set failed=1 & goto done)
pushd frontend
rem Install the page's packages again only when package-lock.json changed
rem since the last time (a copy in node_modules marks it).
fc /b package-lock.json node_modules\.houridle-lock >nul 2>nul || (call npm ci --prefer-offline --no-audit --no-fund && copy /y package-lock.json node_modules\.houridle-lock >nul) || set failed=1
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
