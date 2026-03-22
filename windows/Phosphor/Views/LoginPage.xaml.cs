using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Navigation;
using Phosphor.Services;
using Phosphor.ViewModels;
using Windows.Storage;
using Windows.System;

namespace Phosphor.Views;

public sealed partial class LoginPage : Page
{
    public AuthViewModel ViewModel { get; private set; } = null!;

    public LoginPage()
    {
        InitializeComponent();
    }

    protected override async void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        ViewModel = (AuthViewModel)e.Parameter;

        var savedUrl = ApplicationData.Current.LocalSettings.Values["relay_url"] as string ?? "";
        if (!string.IsNullOrEmpty(savedUrl))
        {
            RelayUrlBox.Text = savedUrl;
            ViewModel.RelayUrl = savedUrl;
            await ConnectToRelay(savedUrl);
        }
    }

    private void RelayUrlBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            _ = TryConnect();
        }
    }

    private async void ConnectButton_Click(object sender, RoutedEventArgs e)
    {
        await TryConnect();
    }

    private async Task TryConnect()
    {
        var url = RelayUrlBox.Text.Trim();
        if (!Uri.TryCreate(url, UriKind.Absolute, out var uri) ||
            uri.Scheme is not ("http" or "https"))
        {
            ErrorBar.Message = "Please enter a valid URL with http:// or https:// (e.g. https://your-relay.example.com)";
            ErrorBar.IsOpen = true;
            return;
        }

        ViewModel.RelayUrl = url;
        ApplicationData.Current.LocalSettings.Values["relay_url"] = url;
        await ConnectToRelay(url);
    }

    private async Task ConnectToRelay(string url)
    {
        ErrorBar.IsOpen = false;
        LoadingRing.IsActive = true;
        ConnectButton.IsEnabled = false;
        RelayHint.Visibility = Visibility.Collapsed;

        try
        {
            ViewModel.RelayUrl = url;
            await ViewModel.LoadProvidersCommand.ExecuteAsync(null);

            if (ViewModel.ErrorMessage is not null)
            {
                ErrorBar.Message = ViewModel.ErrorMessage;
                ErrorBar.IsOpen = true;
                ProviderButtons.Visibility = Visibility.Collapsed;
                RelayHint.Text = "Could not connect — check the URL and try again";
                RelayHint.Visibility = Visibility.Visible;
                return;
            }

            if (ViewModel.Providers.Length == 0)
            {
                ErrorBar.Message = "Relay returned no auth providers.";
                ErrorBar.IsOpen = true;
                ProviderButtons.Visibility = Visibility.Collapsed;
                return;
            }

            // Show provider buttons
            ProviderButtons.Children.Clear();
            ProviderButtons.Children.Add(new TextBlock
            {
                Text = "Sign in with",
                FontSize = 12,
                HorizontalAlignment = HorizontalAlignment.Center,
                Foreground = (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["TextFillColorSecondaryBrush"],
            });

            foreach (var provider in ViewModel.Providers)
            {
                var btn = new Button
                {
                    Content = CapitalizeProvider(provider),
                    HorizontalAlignment = HorizontalAlignment.Stretch,
                    Style = (Style)Application.Current.Resources["AccentButtonStyle"],
                };
                var p = provider;
                btn.Click += async (_, _) => await LoginWithProvider(p);
                ProviderButtons.Children.Add(btn);
            }

            ProviderButtons.Visibility = Visibility.Visible;
        }
        catch (Exception ex)
        {
            ErrorBar.Message = $"Connection failed: {ex.Message}";
            ErrorBar.IsOpen = true;
            ProviderButtons.Visibility = Visibility.Collapsed;
            RelayHint.Text = "Could not connect — check the URL and try again";
            RelayHint.Visibility = Visibility.Visible;
        }
        finally
        {
            LoadingRing.IsActive = false;
            ConnectButton.IsEnabled = true;
        }
    }

    private async Task LoginWithProvider(string provider)
    {
        LoadingRing.IsActive = true;
        ErrorBar.IsOpen = false;

        try
        {
            // Reuse the ViewModel's shared ApiClient (configured with RelayUrl)
            // rather than creating an unmanaged instance that leaks HttpClient.
            var apiClient = App.MainWindow.GetApiClient();
            apiClient.Configure(ViewModel.RelayUrl, "");
            var token = await AuthService.AuthenticateAsync(apiClient, provider, XamlRoot);

            if (token is not null)
            {
                ViewModel.CompleteLogin(token);
                App.MainWindow.NavigateToSessions();
            }
            else
            {
                ErrorBar.Message = "Login cancelled or timed out.";
                ErrorBar.IsOpen = true;
            }
        }
        catch (Exception ex)
        {
            ErrorBar.Message = $"Login failed: {ex.Message}";
            ErrorBar.IsOpen = true;
        }
        finally
        {
            LoadingRing.IsActive = false;
        }
    }

    private static string CapitalizeProvider(string provider) =>
        provider switch
        {
            "microsoft" => "Microsoft",
            "google" => "Google",
            "apple" => "Apple",
            "dev" => "Dev (local)",
            _ => provider,
        };
}
