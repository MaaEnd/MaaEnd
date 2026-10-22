using System;
using System.Drawing;
using System.IO;
using System.Net;
using System.Net.Sockets;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Windows.Forms;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace MaaEndWebHost;

/// <summary>
/// WebView2 宿主：真窗口 + 默认放屏幕外（渲染继续、用户看不见），
/// 通过本地控制口支持 show / park / hide / resume / status / quit。
///
/// 与系统 Edge 后端的差别只有一个：这里的 IsVisible 是进程内 API，
/// hide 是真正的"停车"（不渲染、省 GPU），代价是自动化也跑不了。
/// </summary>
public sealed class HostForm : Form
{
    private readonly HostOptions _o;
    private readonly WebView2 _web = new();
    private StreamWriter? _log;
    private TcpListener? _listener;
    private int _cdpPort;
    private int _controlPort;
    private bool _ready;
    private bool _hotkeys;
    private bool _inTaskbar;
    private readonly HashSet<int> _hotkeyIds = new();
    private const int HOTKEY_HIDE = 1;      // Ctrl+Alt+H：藏/显切换
    private const int HOTKEY_QUIT = 2;      // Ctrl+Alt+Q：优雅退出
    private const int HOTKEY_TASKBAR = 3;   // Ctrl+Alt+T：任务栏/Alt+Tab 可见性切换
    private const uint MOD_ALT = 0x1, MOD_CONTROL = 0x2, MOD_NOREPEAT = 0x4000;
    private const int WM_HOTKEY = 0x0312;

    public HostForm(HostOptions o)
    {
        _o = o;
        _log = OpenLog(o.LogPath);

        Text = "MaaEndWebHost";
        FormBorderStyle = FormBorderStyle.None;   // 不要标题栏：客户区就是 1280x720
        StartPosition = FormStartPosition.Manual;
        ClientSize = new Size(o.Width, o.Height);
        Location = o.Offscreen ? new Point(-32000, -32000) : new Point(120, 120);
        ShowInTaskbar = false;
        BackColor = Color.Black;

        if (o.Hotkeys)
        {
            HandleCreated += (_, __) => RegisterHotkeys();
            FormClosing += (_, __) => UnregisterHotkeys();
        }

        _web.Dock = DockStyle.Fill;
        _web.DefaultBackgroundColor = Color.Black;
        Controls.Add(_web);
        Load += OnLoadAsync;
        FormClosing += (_, __) =>
        {
            try { _listener?.Stop(); } catch { }
            try { _log?.Flush(); _log?.Dispose(); } catch { }
            try { File.Delete(_o.StatePath); } catch { }
        };
    }

    // ---------- 全局热键（无边框窗口没有标题栏，看不见也关不掉，所以给键盘出口） ----------

    /// <summary>注册一条热键；被别的程序占用时换备选组合，而不是整组放弃。</summary>
    private bool TryRegister(int id, int vk, string label)
    {
        if (RegisterHotKey(Handle, id, MOD_CONTROL | MOD_ALT | MOD_NOREPEAT, vk))
        {
            Log($"[hotkey] Ctrl+Alt+{(char)vk} = {label}");
            _hotkeyIds.Add(id);
            return true;
        }
        Log($"[hotkey] Ctrl+Alt+{(char)vk} 注册失败（被占用？）");
        return false;
    }

    private void RegisterHotkeys()
    {
        // 藏/显
        _hotkeys = TryRegister(HOTKEY_HIDE, (int)Keys.H, "藏 / 显")
                   || TryRegister(HOTKEY_HIDE, (int)Keys.OemOpenBrackets, "藏 / 显（备选）");
        // 退出
        _hotkeys &= TryRegister(HOTKEY_QUIT, (int)Keys.Q, "优雅退出")
                    || TryRegister(HOTKEY_QUIT, (int)Keys.OemCloseBrackets, "优雅退出（备选）");
        // 任务栏可见性：Ctrl+Alt+T 常被别的程序占用，备选 Ctrl+Alt+B / Ctrl+Alt+G
        bool tb = TryRegister(HOTKEY_TASKBAR, (int)Keys.T, "任务栏切换")
                  || TryRegister(HOTKEY_TASKBAR, (int)Keys.B, "任务栏切换（备选 B）")
                  || TryRegister(HOTKEY_TASKBAR, (int)Keys.G, "任务栏切换（备选 G）");
        if (!tb) Log("[hotkey] 任务栏切换热键全部被占用；用控制口 status/taskbar 或 webctl.py");
        Log($"[hotkey] {( _hotkeys ? "已就绪" : "部分失败")}：藏/显 + 退出"
            + (tb ? " + 任务栏" : "（任务栏不可用）"));
    }

    private void UnregisterHotkeys()
    {
        foreach (int id in _hotkeyIds)
        {
            try { UnregisterHotKey(Handle, id); } catch { }
        }
        _hotkeyIds.Clear();
        _hotkeys = false;
    }

    protected override void WndProc(ref Message m)
    {
        if (m.Msg == WM_HOTKEY)
        {
            switch (m.WParam.ToInt32())
            {
                case HOTKEY_HIDE:
                    // 藏起来但进程还在：再按一次回来（自动化期间被藏 = 不再渲染）
                    Visible = !Visible;
                    Log($"热键：Visible={Visible}");
                    break;
                case HOTKEY_QUIT:
                    Log("热键：退出");
                    Close();
                    break;
                case HOTKEY_TASKBAR:
                    // 不能用 WinForms 的 ShowInTaskbar：改它会重建窗口句柄，
                    // 热键（注册在旧句柄上）会一起失效。直接改扩展样式。
                    ToggleTaskbar();
                    break;
            }
            return;
        }
        base.WndProc(ref m);
    }

    private static StreamWriter? OpenLog(string path)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(path) ?? ".");
            return new StreamWriter(path, append: true, Encoding.UTF8) { AutoFlush = true };
        }
        catch { return null; }
    }

    private void Log(string msg)
    {
        string line = $"[{DateTime.Now:HH:mm:ss.fff}] {msg}";
        try { _log?.WriteLine(line); } catch { }
        try { Console.WriteLine(line); } catch { }
    }

    // ---------- 浏览器参数与预注入 ----------

    private string BrowserArgs()
    {
        var sb = new StringBuilder();
        sb.Append($"--remote-debugging-port={_o.CdpPort} ");
        // 真窗口会继承系统缩放：不强制 DPR=1 时 CSS 视口凑不出精确的 1280x720
        sb.Append("--force-device-scale-factor=1 ");
        sb.Append("--autoplay-policy=no-user-gesture-required ");
        if (_o.Mute) sb.Append("--mute-audio ");
        sb.Append("--disable-features=msWebOOUI,msPdfOOUI,CalculateNativeWinOcclusion,WebContentsOcclusion ");
        sb.Append("--disable-background-timer-throttling --disable-renderer-backgrounding ");
        sb.Append("--disable-backgrounding-occluded-windows");
        return sb.ToString();
    }

    private string PreInject()
    {
        var sb = new StringBuilder();
        sb.Append("(() => { try {");
        if (_o.HideFloatingBall)
        {
            sb.Append("""
                const css = document.createElement('style');
                css.setAttribute('data-maaend', '1');
                css.textContent = '[class*="e4MbkW"],[class*="hbU_7F"]{display:none!important}';
                (document.head || document.documentElement).appendChild(css);
            """);
        }
        if (_o.BlockPointerLock)
        {
            sb.Append("""
                const block = () => Promise.reject(new Error('blocked by MaaEndWebHost'));
                const P = Element.prototype, H = HTMLElement.prototype;
                if (P.requestPointerLock) P.requestPointerLock = block;
                if (H.requestPointerLock) H.requestPointerLock = block;
                if (P.requestFullscreen) P.requestFullscreen = block;
                if (P.webkitRequestFullscreen) P.webkitRequestFullscreen = block;
            """);
        }
        sb.Append("} catch (e) {} })();");
        return sb.ToString();
    }

    // ---------- 启动 ----------

    private async void OnLoadAsync(object? sender, EventArgs e)
    {
        try
        {
            Directory.CreateDirectory(_o.ProfileDir);
            var env = await CoreWebView2Environment.CreateAsync(
                browserExecutableFolder: null,
                userDataFolder: _o.ProfileDir,
                options: new CoreWebView2EnvironmentOptions { AdditionalBrowserArguments = BrowserArgs() });

            await _web.EnsureCoreWebView2Async(env);
            var core = _web.CoreWebView2;
            // 这版 WinForms 包不暴露 CoreWebView2Controller，停车改用窗体级 Show/Hide：
            // Visible=false 等同 IsVisible=false —— 不渲染、不接收输入、省 GPU。
            if (_o.StartParked) Visible = false;

            var pv = env.BrowserVersionString;
            Log($"webview2 runtime = {pv}");
            Log($"profile = {_o.ProfileDir}");
            Log($"visible = {Visible}（parked={_o.StartParked}）");

            if (!string.IsNullOrEmpty(PreInject()))
            {
                await core.AddScriptToExecuteOnDocumentCreatedAsync(PreInject());
            }

            core.NewWindowRequested += (_, ev) =>
            {
                ev.Handled = true;
                Log($"[popup] {ev.Uri}");
                if (!string.IsNullOrWhiteSpace(ev.Uri)) core.Navigate(ev.Uri);
            };
            core.NavigationCompleted += (_, ev) =>
                Log($"[nav-done] ok={ev.IsSuccess} status={ev.WebErrorStatus} url={core.Source}");
            core.ProcessFailed += (_, ev) =>
            {
                Log($"[process-failed] {ev.ProcessFailedKind}");
                if (ev.ProcessFailedKind == CoreWebView2ProcessFailedKind.BrowserProcessExited) Close();
            };

            core.Navigate(_o.Url);

            ApplyWindowStyle();
            StartControlServer();

            _ready = true;
            WriteState();
            Log($"state = {_o.StatePath}");
            Log($"cdp = http://127.0.0.1:{_cdpPort}  control = 127.0.0.1:{_controlPort}");

            // 新窗口会抢一次前台：记下来，初始化完还回去
            var prev = GetForegroundWindow();
            BeginInvoke(new Action(() =>
            {
                if (prev != IntPtr.Zero && prev != Handle) RestoreForeground(prev);
            }));
        }
        catch (Exception ex)
        {
            Log($"[fatal] {ex.GetType().Name}: {ex.Message}");
            MessageBox.Show(
                "WebView2 初始化失败。\n\n可能原因：未安装 WebView2 Runtime" +
                "（https://developer.microsoft.com/microsoft-edge/webview2/）。\n\n" + ex.Message,
                "MaaEndWebHost", MessageBoxButtons.OK, MessageBoxIcon.Error);
            Close();
        }
    }

    private void WriteState()
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(_o.StatePath) ?? ".");
            var json = JsonSerializer.Serialize(new
            {
                pid = Environment.ProcessId,
                cdp_port = _cdpPort,
                control_port = _controlPort,
                profile = _o.ProfileDir,
                url = _o.Url,
                visible = Visible,
                offscreen = _o.Offscreen,
                runtime = _web.CoreWebView2?.Environment?.BrowserVersionString ?? "",
                started = DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss"),
            }, new JsonSerializerOptions { WriteIndented = true });
            File.WriteAllText(_o.StatePath, json);
        }
        catch (Exception ex) { Log($"[state] 写失败：{ex.Message}"); }
    }

    // ---------- 窗口样式与前台 ----------

    private void ApplyWindowStyle()
    {
        // 不进任务栏 / 不进 Alt+Tab
        long ex = GetWindowLongPtr(Handle, GWL_EXSTYLE).ToInt64();
        SetWindowLongPtr(Handle, GWL_EXSTYLE, new IntPtr((ex & ~WS_EX_APPWINDOW) | WS_EX_TOOLWINDOW));
        _inTaskbar = false;
    }

    private void ToggleTaskbar()
    {
        _inTaskbar = !_inTaskbar;
        long ex = GetWindowLongPtr(Handle, GWL_EXSTYLE).ToInt64();
        long next = _inTaskbar
            ? (ex & ~WS_EX_TOOLWINDOW) | WS_EX_APPWINDOW
            : (ex & ~WS_EX_APPWINDOW) | WS_EX_TOOLWINDOW;
        SetWindowLongPtr(Handle, GWL_EXSTYLE, new IntPtr(next));
        SetWindowPos(Handle, IntPtr.Zero, 0, 0, 0, 0,
                     SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE | SWP_FRAMECHANGED);
        // 任务栏条目是窗口创建时登记的：藏一下再显示才会刷新
        ShowWindow(Handle, SW_HIDE);
        ShowWindow(Handle, SW_SHOWNOACTIVATE);
        Log($"热键：任务栏可见 = {_inTaskbar}（exstyle=0x{(ulong)next:X8}）");
    }

    // ---------- 本地控制口 ----------

    private void StartControlServer()
    {
        _listener = new TcpListener(IPAddress.Loopback, _o.ControlPort);
        _listener.Start();
        _controlPort = ((IPEndPoint)_listener.LocalEndpoint).Port;
        // 端口优先用调用方指定的（可靠）；只有传 0 时才去读 DevToolsActivePort
        _cdpPort = _o.CdpPort > 0 ? _o.CdpPort : ReadCdpPort(_o.ProfileDir);

        var th = new Thread(AcceptLoop) { IsBackground = true, Name = "control" };
        th.Start();
    }

    private static int ReadCdpPort(string profile)
    {
        string file = Path.Combine(profile, "DevToolsActivePort");
        for (int i = 0; i < 100; i++)
        {
            try
            {
                var first = File.ReadAllLines(file);
                if (first.Length > 0 && int.TryParse(first[0].Trim(), out var p)) return p;
            }
            catch { }
            Thread.Sleep(100);
        }
        return 0;
    }

    private void AcceptLoop()
    {
        while (true)
        {
            TcpClient client;
            try { client = _listener!.AcceptTcpClient(); }
            catch { return; }
            try
            {
                using (client)
                using (var stream = client.GetStream())
                {
                    var buf = new byte[4096];
                    int n = stream.Read(buf, 0, buf.Length);
                    string reply = n <= 0 ? "{\"ok\":false,\"error\":\"empty\"}" : HandleCommand(
                        Encoding.UTF8.GetString(buf, 0, n).Trim());
                    var bytes = Encoding.UTF8.GetBytes(reply + "\n");
                    stream.Write(bytes, 0, bytes.Length);
                }
            }
            catch (Exception ex) { Log($"[control] {ex.GetType().Name}: {ex.Message}"); }
        }
    }

    private string HandleCommand(string line)
    {
        string cmd = line;
        int left = 120, top = 120;
        try
        {
            using var doc = JsonDocument.Parse(line);
            cmd = doc.RootElement.TryGetProperty("cmd", out var c) ? c.GetString() ?? "" : "";
            if (doc.RootElement.TryGetProperty("left", out var l)) left = l.GetInt32();
            if (doc.RootElement.TryGetProperty("top", out var t)) top = t.GetInt32();
        }
        catch { /* 允许纯字符串命令 */ }

        string result = "{\"ok\":false,\"error\":\"unknown\"}";
        Invoke(new Action(() =>
        {
            switch (cmd)
            {
                case "show":
                    Location = new Point(left, top);
                    Visible = true;
                    ShowWindow(Handle, SW_SHOWNOACTIVATE);
                    result = StatusJson("shown");
                    break;
                case "park":
                    Location = new Point(-32000, -32000);
                    Visible = true;          // 屏外但仍在渲染，自动化可继续跑
                    result = StatusJson("parked");
                    break;
                case "hide":
                    Visible = false;         // 真停车：不渲染、省 GPU，自动化也跑不了
                    result = StatusJson("hidden");
                    break;
                case "resume":
                    Visible = true;
                    result = StatusJson("resumed");
                    break;
                case "status":
                    result = StatusJson("ok");
                    break;
                case "quit":
                    result = "{\"ok\":true,\"cmd\":\"quit\"}";
                    BeginInvoke(new Action(Close));
                    break;
            }
            Log($"[control] {cmd} -> {result}");
        }));
        return result;
    }

    private string StatusJson(string state) => JsonSerializer.Serialize(new
    {
        ok = true,
        state,
        visible = Visible,
        x = Location.X,
        y = Location.Y,
        width = ClientSize.Width,
        height = ClientSize.Height,
        cdp_port = _cdpPort,
        control_port = _controlPort,
        url = _web.CoreWebView2?.Source ?? "",
        ready = _ready,
    });

    // ---------- Win32 ----------

    private const int GWL_EXSTYLE = -20;
    private const long WS_EX_TOOLWINDOW = 0x00000080;
    private const long WS_EX_APPWINDOW = 0x00040000;
    private const uint SWP_NOSIZE = 0x0001, SWP_NOMOVE = 0x0002, SWP_NOZORDER = 0x0004,
                        SWP_NOACTIVATE = 0x0010, SWP_FRAMECHANGED = 0x0020;
    private const int SW_HIDE = 0, SW_SHOWNOACTIVATE = 4;

    [DllImport("user32.dll", SetLastError = true)]
    private static extern bool RegisterHotKey(IntPtr hWnd, int id, uint fsModifiers, int vk);
    [DllImport("user32.dll", SetLastError = true)]
    private static extern bool UnregisterHotKey(IntPtr hWnd, int id);
    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] private static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")] private static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll", SetLastError = true)]
    private static extern bool SetWindowPos(IntPtr hWnd, IntPtr after, int x, int y, int cx, int cy, uint flags);
    [DllImport("user32.dll", SetLastError = true)]
    private static extern IntPtr GetWindowLongPtr(IntPtr hWnd, int nIndex);
    [DllImport("user32.dll", SetLastError = true)]
    private static extern IntPtr SetWindowLongPtr(IntPtr hWnd, int nIndex, IntPtr dwNewLong);
    [DllImport("kernel32.dll")] private static extern uint GetCurrentThreadId();
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr hWnd, IntPtr pid);
    [DllImport("user32.dll")] private static extern bool AttachThreadInput(uint idAttach, uint idAttachTo, bool attach);

    private static void RestoreForeground(IntPtr target)
    {
        if (SetForegroundWindow(target)) return;
        // 后台进程常被系统拒绝，AttachThreadInput 是标准绕法
        try
        {
            uint cur = GetCurrentThreadId();
            uint fgThread = GetWindowThreadProcessId(GetForegroundWindow(), IntPtr.Zero);
            AttachThreadInput(cur, fgThread, true);
            try { SetForegroundWindow(target); }
            finally { AttachThreadInput(cur, fgThread, false); }
        }
        catch { }
    }
}
