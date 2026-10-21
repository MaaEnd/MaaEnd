using System.Drawing;
using System.IO;
using System.Text;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;

namespace MaaEndWebHost;

/// <summary>
/// 最小 WebView2 宿主：只负责"把浏览器开起来 + 可被 CDP 控制 + 屏外运行"，
/// 不做识别、不做点击——那些交给 CDP 客户端（web/windows/controller）。
/// </summary>
public sealed class HostForm : Form
{
    private readonly HostOptions _o;
    private readonly WebView2 _web = new();
    private StreamWriter? _log;

    public HostForm(HostOptions o)
    {
        _o = o;
        _log = OpenLog(o.LogPath);

        Text = $"MaaEndWebHost :{o.CdpPort}";
        ClientSize = new Size(o.Width, o.Height);
        StartPosition = FormStartPosition.Manual;
        Location = o.Offscreen ? new Point(-32000, -32000) : new Point(60, 60);
        ShowInTaskbar = !o.Offscreen;
        BackColor = Color.Black;

        _web.Dock = DockStyle.Fill;
        _web.DefaultBackgroundColor = Color.Black;
        Controls.Add(_web);
        Load += OnLoadAsync;
        FormClosing += (_, __) => { try { _log?.Flush(); _log?.Dispose(); } catch { } };
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

    private string BrowserArgs()
    {
        var sb = new StringBuilder();
        if (_o.CdpPort > 0)
            sb.Append($"--remote-debugging-port={_o.CdpPort} --remote-allow-origins=* ");
        sb.Append("--autoplay-policy=no-user-gesture-required ");
        if (_o.Mute) sb.Append("--mute-audio ");
        sb.Append("--disable-features=msWebOOUI,msPdfOOUI ");
        sb.Append("--disable-background-timer-throttling --disable-renderer-backgrounding ");
        sb.Append("--disable-backgrounding-occluded-windows ");
        return sb.ToString().Trim();
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
                try {
                  const reject = () => Promise.reject(new Error('blocked by MaaEndWebHost'));
                  Element.prototype.requestPointerLock = reject;
                  if (HTMLElement.prototype.requestPointerLock) HTMLElement.prototype.requestPointerLock = reject;
                } catch (e) {}
            """);
        }
        sb.Append("} catch (e) {} })();");
        return sb.ToString();
    }

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

            Log($"webview2 version = {env.BrowserVersionString}");
            Log($"cdp = http://127.0.0.1:{_o.CdpPort}");
            Log($"profile = {_o.ProfileDir}");

            await core.AddScriptToExecuteOnDocumentCreatedAsync(PreInject());

            core.NewWindowRequested += (_, ev) =>
            {
                ev.Handled = true;
                Log($"[popup] {ev.Uri}");
                if (!string.IsNullOrWhiteSpace(ev.Uri))
                    core.Navigate(ev.Uri);
            };
            core.NavigationStarting += (_, ev) => Log($"[nav] {ev.Uri}");
            core.NavigationCompleted += (_, ev) =>
                Log($"[nav-done] ok={ev.IsSuccess} status={ev.WebErrorStatus} url={core.Source}");
            core.DocumentTitleChanged += (_, __) => Log($"[title] {core.DocumentTitle}");
            core.ProcessFailed += (_, ev) =>
            {
                Log($"[process-failed] {ev.ProcessFailedKind}");
                if (ev.ProcessFailedKind == CoreWebView2ProcessFailedKind.BrowserProcessExited)
                    Close();
            };

            core.Navigate(_o.Url);

            if (_o.Hidden)
            {
                // 实验性：Chromium 可能对不可见窗口节流，实测帧率再决定是否使用
                Log("[warn] --hidden：窗口不可见，可能被节流；优先用 --offscreen");
                Hide();
                ShowInTaskbar = false;
            }
        }
        catch (Exception ex)
        {
            Log($"[fatal] {ex.GetType().Name}: {ex.Message}");
            MessageBox.Show(
                "WebView2 初始化失败。\n\n" +
                "可能原因：未安装 WebView2 Runtime（https://developer.microsoft.com/microsoft-edge/webview2/）。\n\n" +
                ex.Message, "MaaEndWebHost", MessageBoxButtons.OK, MessageBoxIcon.Error);
            Close();
        }
    }
}
