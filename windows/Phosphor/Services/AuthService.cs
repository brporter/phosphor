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

        // Show WebView2 in a ContentDialog.
        // We must initialize CoreWebView2 AFTER the dialog is shown (Loaded),
        // because EnsureCoreWebView2Async requires the control to be in a live visual tree.
        var webView = new WebView2 { Width = 500, Height = 600 };
        var tcs = new TaskCompletionSource<bool>();

        var dialog = new ContentDialog
        {
            Title = $"Sign in with {provider}",
            Content = webView,
            CloseButtonText = "Cancel",
            XamlRoot = xamlRoot,
        };

        webView.Loaded += async (_, _) =>
        {
            try
            {
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
            }
            catch (Exception ex)
            {
                tcs.TrySetException(ex);
                dialog.Hide();
            }
        };

        var dialogResult = await dialog.ShowAsync();
        if (dialogResult == ContentDialogResult.None && !tcs.Task.IsCompleted)
        {
            // User cancelled
            return null;
        }

        // If WebView2 init failed, propagate the exception
        if (tcs.Task.IsFaulted)
            throw tcs.Task.Exception!.InnerException!;

        // Poll for token with retry on transient errors
        const int maxAttempts = 30;
        for (int i = 0; i < maxAttempts; i++)
        {
            ct.ThrowIfCancellationRequested();

            try
            {
                var poll = await api.PollAuthAsync(sessionId, ct);
                if (poll.Status == "complete" && poll.IdToken is not null)
                {
                    return poll.IdToken;
                }
            }
            catch (HttpRequestException) when (i < maxAttempts - 1)
            {
                // Transient network error — wait longer before retrying
                await Task.Delay(2000, ct);
                continue;
            }

            await Task.Delay(1000, ct);
        }

        return null; // Timeout
    }
}
