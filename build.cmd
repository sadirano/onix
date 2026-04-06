@echo off
go build -o onix.exe .
if errorlevel 1 (
    echo Build failed.
    exit /b 1
)
if not exist "%USERPROFILE%\.onix" mkdir "%USERPROFILE%\.onix"
taskkill /f /im onix.exe >nul 2>&1
copy /Y onix.exe "%USERPROFILE%\.onix\onix.exe"
if errorlevel 1 (
    echo Deploy failed - could not copy to %USERPROFILE%\.onix\onix.exe
    exit /b 1
)
echo onix.exe deployed. Installing shortcuts...
"%USERPROFILE%\.onix\onix.exe" shortcuts
echo Done.
