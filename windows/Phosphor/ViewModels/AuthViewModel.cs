using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Phosphor.Models;
using Phosphor.Services;

namespace Phosphor.ViewModels;

public sealed partial class AuthViewModel : ObservableObject
{
    private readonly ApiClient _api;
    private readonly CredentialStore _credentials;

    [ObservableProperty]
    public partial AuthUser? User { get; set; }

    [ObservableProperty]
    public partial string RelayUrl { get; set; } = "";

    [ObservableProperty]
    public partial string[] Providers { get; set; } = [];

    [ObservableProperty]
    public partial string? ErrorMessage { get; set; }

    [ObservableProperty]
    public partial bool IsLoading { get; set; }

    public bool IsAuthenticated => User is not null && !User.IsExpired;

    public AuthViewModel(ApiClient api, CredentialStore credentials)
    {
        _api = api;
        _credentials = credentials;
    }

    /// <summary>
    /// Try to restore a saved session on app launch.
    /// </summary>
    public void TryRestoreSession(string relayUrl)
    {
        RelayUrl = relayUrl;
        var host = new Uri(relayUrl).Host;
        var cred = _credentials.Load(host);
        if (cred is null) return;

        try
        {
            var user = AuthUser.FromJwt(cred.IdToken);
            if (!user.IsExpired)
            {
                User = user;
                _api.Configure(relayUrl, cred.IdToken);
                OnPropertyChanged(nameof(IsAuthenticated));
            }
            else
            {
                _credentials.TryRemove(host);
            }
        }
        catch
        {
            _credentials.TryRemove(host);
        }
    }

    /// <summary>
    /// Fetch available auth providers from relay.
    /// </summary>
    [RelayCommand]
    private async Task LoadProvidersAsync()
    {
        ErrorMessage = null;
        IsLoading = true;
        try
        {
            _api.Configure(RelayUrl, "");
            var config = await _api.GetAuthConfigAsync();
            Providers = config.Providers;
        }
        catch (Exception ex)
        {
            ErrorMessage = $"Failed to connect to relay: {ex.Message}";
        }
        finally
        {
            IsLoading = false;
        }
    }

    /// <summary>
    /// Complete login after receiving token from WebView2 auth flow.
    /// </summary>
    public void CompleteLogin(string idToken)
    {
        var user = AuthUser.FromJwt(idToken);
        User = user;
        _api.Configure(RelayUrl, idToken);

        var host = new Uri(RelayUrl).Host;
        _credentials.Save(host, idToken, RelayUrl);

        ErrorMessage = null;
        OnPropertyChanged(nameof(IsAuthenticated));
    }

    [RelayCommand]
    private void Logout()
    {
        var host = new Uri(RelayUrl).Host;
        _credentials.TryRemove(host);
        User = null;
        _api.Configure(RelayUrl, "");
        OnPropertyChanged(nameof(IsAuthenticated));
    }
}
