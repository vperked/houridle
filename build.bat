@echo off
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w" -o houridle.exe ./src && echo Built houridle.exe
