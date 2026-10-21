@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set EXE=MaaEndWebHost\bin\Release\net8.0-windows\MaaEndWebHost.exe
if not exist "%EXE%" (
    echo [首次运行] 未找到宿主，正在构建……
    pushd MaaEndWebHost
    dotnet build -c Release --nologo
    popd
    if not exist "%EXE%" (
        echo 构建失败，请检查是否安装了 .NET 8 SDK。
        pause
        exit /b 1
    )
)

echo 启动 MaaEndWebHost（屏外 + CDP 9338）…
start "" "%EXE%" --offscreen --cdp-port 9338 --log "%~dp0host.log"
echo 已启动。验证：curl http://127.0.0.1:9338/json
timeout /t 3 >nul
