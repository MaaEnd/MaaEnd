using System;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace MaaEndWebHost;

internal static class Program
{
    [DllImport("kernel32.dll")]
    private static extern bool AllocConsole();

    [STAThread]
    private static void Main(string[] args)
    {
        var opts = HostOptions.Parse(args);
        if (opts.ShowConsole) AllocConsole();

        ApplicationConfiguration.Initialize();
        Application.Run(new HostForm(opts));
    }
}
