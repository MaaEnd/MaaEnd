using System;
using System.IO;
using System.Text;

namespace MaaEndWebHost;

/// <summary>宿主命令行参数。所有端口都允许 0 = 自动分配。</summary>
public sealed class HostOptions
{
    public string Url { get; private set; } = "https://endfield.hypergryph.com/cloud";
    public int CdpPort { get; private set; }
    public int ControlPort { get; private set; }
    public string ProfileDir { get; private set; } =
        Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "MaaEnd", "webview2-profile");
    public int Width { get; private set; } = 1280;
    public int Height { get; private set; } = 720;
    public bool Mute { get; private set; } = true;
    public bool ShowConsole { get; private set; }
    public bool Hotkeys { get; private set; } = true;
    public bool BlockPointerLock { get; private set; }
    public bool HideFloatingBall { get; private set; } = true;
    /// <summary>窗口放到屏幕外但保持可见（渲染继续，用户看不见）。</summary>
    public bool Offscreen { get; private set; } = true;
    /// <summary>启动即 IsVisible=false（真正停车：不渲染、省 GPU，但自动化也跑不了）。</summary>
    public bool StartParked { get; private set; }
    public string LogPath { get; private set; } =
        Path.Combine(AppContext.BaseDirectory, "host.log");
    public string StatePath { get; private set; } = "";

    public static HostOptions Parse(string[] args)
    {
        var o = new HostOptions();
        for (int i = 0; i < args.Length; i++)
        {
            string a = args[i];
            string? inline = null;
            int eq = a.IndexOf('=');
            if (a.StartsWith("--") && eq > 0)
            {
                inline = a[(eq + 1)..];
                a = a[..eq];
            }
            string? Next() => inline ?? (i + 1 < args.Length ? args[++i] : null);

            switch (a)
            {
                case "--url": o.Url = Next() ?? o.Url; break;
                case "--cdp-port": o.CdpPort = int.TryParse(Next(), out var p) ? p : o.CdpPort; break;
                case "--control-port": o.ControlPort = int.TryParse(Next(), out var c) ? c : o.ControlPort; break;
                case "--profile": o.ProfileDir = Next() ?? o.ProfileDir; break;
                case "--on-screen": o.Offscreen = false; break;
                case "--parked": o.StartParked = true; break;
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
            case "--no-hotkeys": o.Hotkeys = false; break;
                case "--log": o.LogPath = Next() ?? o.LogPath; break;
                case "--state": o.StatePath = Next() ?? o.StatePath; break;
                case "--help":
                case "-h":
                    Console.WriteLine(Help);
                    Environment.Exit(0);
                    break;
            }
        }

        if (string.IsNullOrEmpty(o.StatePath))
        {
            o.StatePath = Path.Combine(o.ProfileDir, "host-state.json");
        }
        return o;
    }

    public const string Help = """
    MaaEndWebHost —— 云·终末地网页端 WebView2 宿主

    --url <URL>            起始地址（默认云终末地网页端）
    --cdp-port <N>         CDP 端口（0 = 自动分配）
    --control-port <N>     控制口端口（0 = 自动分配）；按行 JSON 收发 show/park/hide/status/quit
    --profile <DIR>        WebView2 用户数据目录
    --size <WxH>           视口（默认 1280x720）
    --on-screen            窗口放在屏幕内（默认放屏幕外）
    --parked               启动即 IsVisible=false（停车：不渲染、自动化也不能跑）
    --no-mute              不静音
    --console              附加控制台看日志
    --block-pointer-lock   预注入禁掉 requestPointerLock / requestFullscreen
    --keep-ball            不隐藏云端悬浮球
    --log <FILE>           日志路径
    --state <FILE>         状态文件（pid/端口），默认 <profile>/host-state.json
    """;
}
