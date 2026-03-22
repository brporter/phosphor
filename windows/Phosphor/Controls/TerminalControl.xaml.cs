using System;
using System.Text.Json;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Web.WebView2.Core;

namespace Phosphor.Controls;

/// <summary>
/// Terminal rendering control using WebView2 + xterm.js.
/// The design spec lists Microsoft.Terminal.Control (OpenConsole) as the primary renderer,
/// but it proved too tightly coupled to Windows Terminal internals for embedding.
/// This WebView2 fallback uses the same xterm.js renderer as the web SPA.
/// TODO: Revisit OpenConsole integration if Microsoft.Terminal.Control becomes easier to embed.
/// </summary>
public sealed partial class TerminalControl : UserControl
{
    private bool _isReady;

    public event Action<byte[]>? InputReceived;
    public event Action<int, int>? TerminalSizeChanged;
    public event Action? Ready;

    public TerminalControl()
    {
        InitializeComponent();
        Loaded += TerminalControl_Loaded;
    }

    private async void TerminalControl_Loaded(object sender, RoutedEventArgs e)
    {
        await WebView.EnsureCoreWebView2Async();

        // Map a virtual hostname to only the Controls directory (not the entire app directory)
        var controlsDir = System.IO.Path.Combine(AppContext.BaseDirectory, "Controls");
        WebView.CoreWebView2.SetVirtualHostNameToFolderMapping(
            "phosphor.local",
            controlsDir,
            CoreWebView2HostResourceAccessKind.DenyCors);

        WebView.CoreWebView2.WebMessageReceived += CoreWebView2_WebMessageReceived;

        // Suppress default browser context menu
        WebView.CoreWebView2.Settings.AreDefaultContextMenusEnabled = false;

        // Block navigation to external URLs
        WebView.CoreWebView2.NavigationStarting += (_, args) =>
        {
            var uri = new Uri(args.Uri);
            if (uri.Host != "phosphor.local")
            {
                args.Cancel = true;
            }
        };

        // Navigate to the bundled terminal.html served from the Controls/ virtual host
        WebView.CoreWebView2.Navigate("https://phosphor.local/terminal.html");
    }

    private void CoreWebView2_WebMessageReceived(CoreWebView2 sender, CoreWebView2WebMessageReceivedEventArgs args)
    {
        var json = args.WebMessageAsJson;
        using var doc = JsonDocument.Parse(json);
        var root = doc.RootElement;
        var type = root.GetProperty("type").GetString();

        switch (type)
        {
            case "input":
            {
                var dataArray = root.GetProperty("data");
                var bytes = new byte[dataArray.GetArrayLength()];
                var i = 0;
                foreach (var element in dataArray.EnumerateArray())
                {
                    bytes[i++] = (byte)element.GetInt32();
                }
                InputReceived?.Invoke(bytes);
                break;
            }
            case "resize":
            {
                var cols = root.GetProperty("cols").GetInt32();
                var rows = root.GetProperty("rows").GetInt32();
                TerminalSizeChanged?.Invoke(cols, rows);
                break;
            }
            case "ready":
            {
                _isReady = true;
                Ready?.Invoke();
                break;
            }
        }
    }

    public void WriteOutput(ReadOnlyMemory<byte> data)
    {
        if (!_isReady) return;

        var base64 = Convert.ToBase64String(data.Span);
        var message = $"{{\"type\":\"write\",\"data\":\"{base64}\"}}";
        DispatcherQueue.TryEnqueue(() =>
        {
            WebView.CoreWebView2?.PostWebMessageAsJson(message);
        });
    }

    public void SetSize(int cols, int rows)
    {
        if (!_isReady) return;

        var message = $"{{\"type\":\"resize\",\"cols\":{cols},\"rows\":{rows}}}";
        DispatcherQueue.TryEnqueue(() =>
        {
            WebView.CoreWebView2?.PostWebMessageAsJson(message);
        });
    }

    public void Clear()
    {
        if (!_isReady) return;

        const string message = "{\"type\":\"clear\"}";
        DispatcherQueue.TryEnqueue(() =>
        {
            WebView.CoreWebView2?.PostWebMessageAsJson(message);
        });
    }
}
