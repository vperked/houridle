@echo off
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w" -o houridle.exe ./backend && echo Built houridle.exe
set GOOS=linux
go build -trimpath -ldflags "-s -w" -o houridle ./backend && echo Built houridle (Linux)
