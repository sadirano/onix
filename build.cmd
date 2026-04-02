@echo off
go build -o onix.exe .
if errorlevel 1 (
    echo Build failed.
    exit /b 1
)
if not exist "%USERPROFILE%\.onix" mkdir "%USERPROFILE%\.onix"
copy onix.exe "%USERPROFILE%\.onix\onix.exe"
echo Done. onix.exe deployed to %USERPROFILE%\.onix\
