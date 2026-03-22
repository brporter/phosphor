using System.Collections.ObjectModel;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Phosphor.Models;
using Phosphor.Services;

namespace Phosphor.ViewModels;

public sealed partial class SessionListViewModel : ObservableObject, IDisposable
{
    private readonly ApiClient _api;
    private PeriodicTimer? _pollTimer;
    private CancellationTokenSource? _cts;

    public ObservableCollection<SessionData> Sessions { get; } = [];

    [ObservableProperty]
    public partial bool IsLoading { get; set; }

    [ObservableProperty]
    public partial string? ErrorMessage { get; set; }

    [ObservableProperty]
    public partial bool IsEmpty { get; set; }

    public Action<string>? NavigateToSession { get; set; }

    public SessionListViewModel(ApiClient api)
    {
        _api = api;
    }

    /// <summary>
    /// Start polling for sessions every 5 seconds.
    /// </summary>
    public void StartPolling()
    {
        _cts = new CancellationTokenSource();
        _pollTimer = new PeriodicTimer(TimeSpan.FromSeconds(5));
        _ = PollLoopAsync(_cts.Token);

        // Fetch immediately
        _ = RefreshAsync();
    }

    public void StopPolling()
    {
        _cts?.Cancel();
        _pollTimer?.Dispose();
        _pollTimer = null;
    }

    private async Task PollLoopAsync(CancellationToken ct)
    {
        try
        {
            while (_pollTimer is not null && await _pollTimer.WaitForNextTickAsync(ct))
            {
                await RefreshAsync();
            }
        }
        catch (OperationCanceledException) { }
    }

    [RelayCommand]
    private async Task RefreshAsync()
    {
        try
        {
            IsLoading = Sessions.Count == 0;
            ErrorMessage = null;

            var sessions = await _api.GetSessionsAsync();

            Sessions.Clear();
            foreach (var s in sessions)
            {
                Sessions.Add(s);
            }

            IsEmpty = Sessions.Count == 0;
        }
        catch (Exception ex)
        {
            ErrorMessage = $"Failed to load sessions: {ex.Message}";
        }
        finally
        {
            IsLoading = false;
        }
    }

    [RelayCommand]
    private async Task DestroySessionAsync(string sessionId)
    {
        try
        {
            await _api.DestroySessionAsync(sessionId);
            var session = Sessions.FirstOrDefault(s => s.Id == sessionId);
            if (session is not null) Sessions.Remove(session);
            IsEmpty = Sessions.Count == 0;
        }
        catch (Exception ex)
        {
            ErrorMessage = $"Failed to destroy session: {ex.Message}";
        }
    }

    [RelayCommand]
    private void OpenSession(string sessionId)
    {
        NavigateToSession?.Invoke(sessionId);
    }

    public void Dispose()
    {
        StopPolling();
        _cts?.Dispose();
    }
}
