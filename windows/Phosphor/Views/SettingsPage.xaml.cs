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

        // Disable API key generation until relay support is added
        GenerateKeyButton.IsEnabled = false;
        GenerateKeyButton.Content = "Generate API Key (coming soon)";
    }

    private void GenerateKey_Click(object sender, RoutedEventArgs e)
    {
        // TODO: Implement when relay adds POST /api/auth/api-key endpoint.
        // Button is disabled until then — see OnNavigatedTo.
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
