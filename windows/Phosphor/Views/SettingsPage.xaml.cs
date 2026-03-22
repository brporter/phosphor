using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Phosphor.ViewModels;

namespace Phosphor.Views;

public sealed partial class SettingsPage : Page
{
    private AuthViewModel? _viewModel;

    public SettingsPage()
    {
        InitializeComponent();
    }

    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        _viewModel = (AuthViewModel)e.Parameter;

        EmailText.Text = _viewModel.User?.Email ?? "\u2014";
        IssuerText.Text = _viewModel.User?.Issuer ?? "\u2014";
        RelayUrlBox.Text = _viewModel.RelayUrl;
    }

    private async void GenerateKey_Click(object sender, RoutedEventArgs e)
    {
        try
        {
            // POST /api/auth/api-key — requires adding this to ApiClient
            // For now, show the UI flow; actual API call added during integration
            GenerateKeyButton.IsEnabled = false;
            // var response = await _api.GenerateApiKeyAsync();
            // ApiKeyBox.Text = response.Key;
            ApiKeyBox.Text = "(API key generation requires relay support)";
            KeyDisplay.Visibility = Visibility.Visible;
        }
        catch (Exception ex)
        {
            ApiKeyBox.Text = $"Error: {ex.Message}";
            KeyDisplay.Visibility = Visibility.Visible;
        }
        finally
        {
            GenerateKeyButton.IsEnabled = true;
        }
    }

    private void CopyKey_Click(object sender, RoutedEventArgs e)
    {
        var dataPackage = new Windows.ApplicationModel.DataTransfer.DataPackage();
        dataPackage.SetText(ApiKeyBox.Text);
        Windows.ApplicationModel.DataTransfer.Clipboard.SetContent(dataPackage);
    }

    private void SignOut_Click(object sender, RoutedEventArgs e)
    {
        _viewModel?.LogoutCommand.Execute(null);

        App.MainWindow.NavigateToLogin();
    }
}
