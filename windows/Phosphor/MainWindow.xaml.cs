using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Animation;
using Phosphor.Services;
using Phosphor.ViewModels;
using Phosphor.Views;
using Windows.Storage;

namespace Phosphor;

public sealed partial class MainWindow : Window
{
    private readonly ApiClient _api = new();
    private readonly CredentialStore _credentials = new();
    private readonly AuthViewModel _authVm;

    public MainWindow()
    {
        InitializeComponent();

        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        var appWindow = this.AppWindow;
        appWindow.Resize(new Windows.Graphics.SizeInt32(1024, 768));

        TrySetMicaBackdrop();
        RestoreWindowState();

        _authVm = new AuthViewModel(_api, _credentials);

        var relayUrl = ApplicationData.Current.LocalSettings.Values["relay_url"] as string ?? "";
        if (!string.IsNullOrEmpty(relayUrl))
        {
            _authVm.TryRestoreSession(relayUrl);
        }

        if (_authVm.IsAuthenticated)
        {
            NavigateToSessions();
        }
        else
        {
            NavigateToLogin();
        }

        Title = "Phosphor";
    }

    private void TrySetMicaBackdrop()
    {
        if (Microsoft.UI.Composition.SystemBackdrops.MicaController.IsSupported())
        {
            SystemBackdrop = new Microsoft.UI.Xaml.Media.MicaBackdrop();
        }
    }

    private void RestoreWindowState()
    {
        var settings = ApplicationData.Current.LocalSettings;
        if (settings.Values.TryGetValue("window_width", out var w) &&
            settings.Values.TryGetValue("window_height", out var h))
        {
            try
            {
                AppWindow.Resize(new Windows.Graphics.SizeInt32(
                    Convert.ToInt32(w), Convert.ToInt32(h)));
            }
            catch (Exception)
            {
                // Fallback to default size if stored values have unexpected type
            }
        }

        Closed += (_, _) =>
        {
            var size = AppWindow.Size;
            settings.Values["window_width"] = size.Width;
            settings.Values["window_height"] = size.Height;
        };
    }

    public ApiClient GetApiClient() => _api;

    public void NavigateToLogin()
    {
        // Show login frame, hide nav
        NavView.Visibility = Visibility.Collapsed;
        LoginFrame.Visibility = Visibility.Visible;
        LoginFrame.Navigate(typeof(LoginPage), _authVm,
            new SlideNavigationTransitionInfo { Effect = SlideNavigationTransitionEffect.FromRight });
    }

    public void NavigateToSessions()
    {
        // Show nav, hide login frame
        LoginFrame.Visibility = Visibility.Collapsed;
        LoginFrame.Content = null;
        NavView.Visibility = Visibility.Visible;
        NavView.SelectedItem = NavView.MenuItems[0];
        NavContentFrame.Navigate(typeof(SessionListPage),
            new SessionListViewModel(_api),
            new SlideNavigationTransitionInfo { Effect = SlideNavigationTransitionEffect.FromRight });
    }

    public void NavigateToTerminal(string sessionId)
    {
        var vm = new TerminalViewModel(_api);
        NavContentFrame.Navigate(typeof(TerminalPage), (vm, sessionId),
            new DrillInNavigationTransitionInfo());
        NavView.IsBackEnabled = NavContentFrame.CanGoBack;
    }

    public void NavigateToSettings()
    {
        NavContentFrame.Navigate(typeof(SettingsPage), _authVm,
            new SlideNavigationTransitionInfo { Effect = SlideNavigationTransitionEffect.FromLeft });
    }

    private void NavView_SelectionChanged(NavigationView sender, NavigationViewSelectionChangedEventArgs args)
    {
        if (args.SelectedItem is NavigationViewItem item)
        {
            switch (item.Tag?.ToString())
            {
                case "sessions":
                    NavigateToSessions();
                    break;
                case "settings":
                    NavigateToSettings();
                    break;
            }
        }
    }

    private void NavView_BackRequested(NavigationView sender, NavigationViewBackRequestedEventArgs args)
    {
        if (NavContentFrame.CanGoBack)
        {
            NavContentFrame.GoBack();
            NavView.IsBackEnabled = NavContentFrame.CanGoBack;
        }
    }
}
