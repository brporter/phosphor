using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Web.WebView2.Core;
using Phosphor.Models;

namespace Phosphor.Services;

/// <summary>
/// Handles the relay-mediated OIDC flow using a WebView2 popup window.
/// </summary>
public static class AuthService
{
    /// <summary>
    /// Open a WebView2 popup, navigate to the auth URL, and wait for the
    /// phosphor:// callback redirect. Returns the id_token on success.
    /// </summary>
    public static async Task<string?> AuthenticateAsync(
        ApiClient api, string provider, XamlRoot xamlRoot, CancellationToken ct = default)
    {
        // Initiate login
        var loginResponse = await api.LoginAsync(provider, ct);
        var sessionId = loginResponse.SessionId;
        var authUrl = loginResponse.AuthUrl;

        // Show WebView2 in a ContentDialog
        var webView = new WebView2 { Width = 500, Height = 600 };
        var dialog = new ContentDialog
        {
            Title = $"Sign in with {provider}",
            Content = webView,
            CloseButtonText = "Cancel",
            XamlRoot = xamlRoot,
        };

        var tcs = new TaskCompletionSource<bool>();

        await webView.EnsureCoreWebView2Async();
        webView.CoreWebView2.NavigationStarting += (_, args) =>
        {
            if (args.Uri.StartsWith("phosphor://auth/callback", StringComparison.OrdinalIgnoreCase))
            {
                args.Cancel = true; // Suppress navigation to custom scheme
                tcs.TrySetResult(true);
                dialog.Hide();
            }
        };

        webView.CoreWebView2.Navigate(authUrl);

        var dialogResult = await dialog.ShowAsync();
        if (dialogResult == ContentDialogResult.None && !tcs.Task.IsCompleted)
        {
            // User cancelled
            return null;
        }

        // Poll for token
        for (int i = 0; i < 30; i++) // 30 attempts, 1s apart = 30s timeout
        {
            ct.ThrowIfCancellationRequested();
            var poll = await api.PollAuthAsync(sessionId, ct);
            if (poll.Status == "complete" && poll.IdToken is not null)
            {
                return poll.IdToken;
            }
            await Task.Delay(1000, ct);
        }

        return null; // Timeout
    }
}
