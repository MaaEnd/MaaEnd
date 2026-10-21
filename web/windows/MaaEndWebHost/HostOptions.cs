using System;
using System.IO;

namespace MaaEndWebHost;

/// <summary>命令行参数。默认值即"能跑"的配置：屏外窗口 + CDP 9338 + 独立 profile。</summary>
public sealed class HostOptions
{
    public string Url { get; private set; } = "https://endfield.hypergryph.com/cloud";
    public int CdpPort { get; private set; } = 9338;
    public string ProfileDir { get; private set; } =
        Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                     "MaaEndWebHost", "udf");
    public bool Offscreen { get; private set; }
    public bool Hidden { get; private set; }
    public int Width { get; private set; } = 1280;
    public int Height { get; private set; } = 720;
    public bool Mute { get; private set; } = true;
    public bool ShowConsole { get; private set; }
    public bool BlockPointerLock { get; private set; }
    public bool HideFloatingBall { get; private set; } = true;
    public string LogPath { get; private set; } =
        Path.Combine(AppContext.BaseDirectory, "host.log");

    public static HostOptions Parse(string[] args)
    {
        var o = new HostOptions();
        for (int i = 0; i < args.Length; i++)
        {
            string a = args[i];
            string? Next() => i + 1 < args.Length ? args[++i] : null;

            switch (a)
            {
                case "--url": o.Url = Next() ?? o.Url; break;
                case "--cdp-port": o.CdpPort = int.TryParse(Next(), out var p) ? p : o.CdpPort; break;
                case "--profile": o.ProfileDir = Next() ?? o.ProfileDir; break;
                case "--offscreen": o.Offscreen = true; break;
                case "--hidden": o.Hidden = true; break;
                case "--size":
                    var s = Next();
                    if (s != null && s.Contains('x'))
                    {
                        var parts = s.Split('x');
                        if (int.TryParse(parts[0], out var w)) o.Width = w;
                        if (int.TryParse(parts[1], out var h)) o.Height = h;
                    }
                    break;
                case "--no-mute": o.Mute = false; break;
                case "--console": o.ShowConsole = true; break;
                case "--block-pointer-lock": o.BlockPointerLock = true; break;
                case "--keep-ball": o.HideFloatingBall = false; break;
                case "--log": o.LogPath = Next() ?? o.LogPath; break;
                case "--help":
                case "-h":
                    Console.WriteLine(Help);
                    Environment.Exit(0);
                    break;
            }
        }
        return o;
    }

    public const string Help = """
MaaEndWebHost —— 云·终末地网页端 WebView2 宿主

  --url <URL>              起始地址（默认 https://endfield.hypergryph.com/cloud）
  --cdp-port <N>           CDP 端口（默认 9338，0 = 不开放）
  --profile <DIR>          WebView2 用户数据目录（默认 %LOCALAPPDATA%\MaaEndWebHost\udf）
  --size <WxH>             视口（默认 1280x720）
  --offscreen              窗口移到屏幕外（推荐的后台方式）
  --hidden                 隐藏窗口（实验性：可能被 Chromium 节流降帧）
  --no-mute                不静音
  --console                附加控制台，便于看日志
  --block-pointer-lock     预注入脚本里禁掉 requestPointerLock（后台模式用）
  --keep-ball              不隐藏云端悬浮球
  --log <FILE>             日志路径（默认 exe 同目录 host.log）
""";
}
