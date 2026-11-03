@echo off
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w" -o houridle.exe ./src && echo Built houridle.exe
set GOOS=linux
go build -trimpath -ldflags "-s -w" -o houridle ./src && echo Built houridle (Linux)
