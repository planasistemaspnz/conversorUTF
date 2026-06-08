@echo off
setlocal

echo [1/2] Baixando dependencias...
go mod tidy
if errorlevel 1 goto :error

echo [2/2] Gerando conversor.exe...
go build -o conversor.exe .
if errorlevel 1 goto :error

echo Build concluido: conversor.exe
exit /b 0

:error
echo Falha no build.
exit /b 1
