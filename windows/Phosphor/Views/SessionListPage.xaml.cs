using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Phosphor.Models;
using Phosphor.ViewModels;

namespace Phosphor.Views;

public sealed partial class SessionListPage : Page
{
    private SessionListViewModel? _viewModel;

    public SessionListPage()
    {
        InitializeComponent();
    }

    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        _viewModel = (SessionListViewModel)e.Parameter;

        SessionList.ItemsSource = _viewModel.Sessions;
        _viewModel.NavigateToSession = sessionId =>
        {
            MainWindow.NavigateToTerminal(sessionId);
        };

        _viewModel.StartPolling();
        UpdateVisibility();
    }

    protected override void OnNavigatedFrom(NavigationEventArgs e)
    {
        base.OnNavigatedFrom(e);
        _viewModel?.Dispose();
    }

    private void SessionList_ItemClick(object sender, ItemClickEventArgs e)
    {
        if (e.ClickedItem is SessionData session)
        {
            _viewModel?.OpenSessionCommand.Execute(session.Id);
        }
    }

    private async void Refresh_Click(object sender, RoutedEventArgs e)
    {
        if (_viewModel is not null)
        {
            await _viewModel.RefreshCommand.ExecuteAsync(null);
            UpdateVisibility();
        }
    }

    private void UpdateVisibility()
    {
        if (_viewModel is null) return;
        LoadingRing.IsActive = _viewModel.IsLoading;
        EmptyState.Visibility = _viewModel.IsEmpty && !_viewModel.IsLoading
            ? Visibility.Visible : Visibility.Collapsed;
        SessionList.Visibility = !_viewModel.IsEmpty
            ? Visibility.Visible : Visibility.Collapsed;

        if (_viewModel.ErrorMessage is not null)
        {
            ErrorBar.Message = _viewModel.ErrorMessage;
            ErrorBar.IsOpen = true;
        }
    }

    private static MainWindow MainWindow => App.MainWindow;
}
