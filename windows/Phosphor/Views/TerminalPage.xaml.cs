using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Navigation;
using Phosphor.ViewModels;
using Windows.ApplicationModel.DataTransfer;
using Windows.Storage;
using Windows.Storage.Pickers;

namespace Phosphor.Views;

public sealed partial class TerminalPage : Page
{
    private TerminalViewModel? _viewModel;
    private string _sessionId = "";
    private bool _isSubscribed;
    private bool _passphraseDialogShowing;

    // Store delegate references for unsubscription
    private Action<byte[]>? _inputHandler;
    private Action<int, int>? _sizeHandler;
    private Action? _readyHandler;

    public TerminalPage()
    {
        InitializeComponent();
    }

    protected override void OnNavigatedTo(NavigationEventArgs e)
    {
        base.OnNavigatedTo(e);
        var (vm, sessionId) = ((TerminalViewModel, string))e.Parameter;
        _viewModel = vm;
        _sessionId = sessionId;

        // Wire up ViewModel state changes
        _viewModel.PropertyChanged += ViewModel_PropertyChanged;

        // Wire terminal output to the TerminalControl
        _viewModel.WriteToTerminal = data =>
        {
            TerminalControl.WriteOutput(data);
        };

        _viewModel.SetTerminalSize = (cols, rows) =>
        {
            TerminalControl.SetSize(cols, rows);
        };

        // Only subscribe to TerminalControl events once
        if (!_isSubscribed)
        {
            _inputHandler = async (bytes) =>
            {
                if (_viewModel is not null)
                {
                    await _viewModel.SendInputAsync(bytes);
                }
            };

            _sizeHandler = async (cols, rows) =>
            {
                if (_viewModel is not null)
                {
                    await _viewModel.SendResizeAsync(cols, rows);
                }
            };

            _readyHandler = async () =>
            {
                if (_viewModel is not null)
                {
                    await _viewModel.ConnectAsync(_sessionId);
                }
            };

            TerminalControl.InputReceived += _inputHandler;
            TerminalControl.TerminalSizeChanged += _sizeHandler;
            TerminalControl.Ready += _readyHandler;
            _isSubscribed = true;
        }
    }

    protected override async void OnNavigatedFrom(NavigationEventArgs e)
    {
        base.OnNavigatedFrom(e);

        // Unsubscribe TerminalControl events
        if (_isSubscribed)
        {
            if (_inputHandler is not null)
                TerminalControl.InputReceived -= _inputHandler;
            if (_sizeHandler is not null)
                TerminalControl.TerminalSizeChanged -= _sizeHandler;
            if (_readyHandler is not null)
                TerminalControl.Ready -= _readyHandler;
            _isSubscribed = false;
        }

        if (_viewModel is not null)
        {
            _viewModel.PropertyChanged -= ViewModel_PropertyChanged;
            await _viewModel.DisconnectAsync();
            _viewModel.Dispose();
        }
    }

    private void ViewModel_PropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
    {
        DispatcherQueue.TryEnqueue(() => UpdateUI(e.PropertyName));
    }

    private void UpdateUI(string? property)
    {
        if (_viewModel is null) return;

        switch (property)
        {
            case nameof(TerminalViewModel.ConnectionState):
                StatusText.Text = _viewModel.ConnectionState switch
                {
                    "connecting" => "Connecting...",
                    "connected" => "Connected",
                    "disconnected" => "Disconnected",
                    "error" => _viewModel.ErrorMessage ?? "Error",
                    _ => _viewModel.ConnectionState,
                };
                StatusDot.Fill = _viewModel.ConnectionState switch
                {
                    "connected" => (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["PhosphorGreenBrush"],
                    "connecting" => (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["PhosphorAmberBrush"],
                    _ => (Microsoft.UI.Xaml.Media.Brush)Application.Current.Resources["PhosphorRedBrush"],
                };
                PlaceholderText.Visibility = _viewModel.ConnectionState == "connected"
                    ? Visibility.Collapsed : Visibility.Visible;
                break;

            case nameof(TerminalViewModel.Command):
                CommandText.Text = _viewModel.Command;
                break;

            case nameof(TerminalViewModel.ViewerCount):
                ViewerCountText.Text = $"\U0001F441 {_viewModel.ViewerCount}";
                break;

            case nameof(TerminalViewModel.IsEncrypted):
                EncryptedBadge.Visibility = _viewModel.IsEncrypted
                    ? Visibility.Visible : Visibility.Collapsed;
                break;

            case nameof(TerminalViewModel.IsPty):
                UploadButton.Visibility = _viewModel.IsPty
                    ? Visibility.Visible : Visibility.Collapsed;
                break;

            case nameof(TerminalViewModel.HasExited):
                ExitOverlay.Visibility = _viewModel.HasExited
                    ? Visibility.Visible : Visibility.Collapsed;
                if (_viewModel.ExitCode.HasValue)
                    ExitCodeText.Text = $"Process exited with code {_viewModel.ExitCode.Value}";
                break;

            case nameof(TerminalViewModel.NeedsPassphrase):
                if (_viewModel.NeedsPassphrase && !_passphraseDialogShowing)
                    _ = ShowPassphraseDialog();
                break;
        }
    }

    private async Task ShowPassphraseDialog()
    {
        if (_passphraseDialogShowing) return;
        _passphraseDialogShowing = true;

        try
        {
            var input = new PasswordBox { PlaceholderText = "Enter passphrase" };
            var dialog = new ContentDialog
            {
                Title = "Encrypted Session",
                Content = input,
                PrimaryButtonText = "Decrypt",
                CloseButtonText = "Cancel",
                XamlRoot = XamlRoot,
            };

            var result = await dialog.ShowAsync();
            if (result == ContentDialogResult.Primary && _viewModel is not null)
            {
                _viewModel.SubmitPassphraseCommand.Execute(input.Password);
            }
            else
            {
                // User cancelled — disconnect
                if (_viewModel is not null)
                {
                    await _viewModel.DisconnectAsync();
                    if (Frame.CanGoBack) Frame.GoBack();
                }
            }
        }
        finally
        {
            _passphraseDialogShowing = false;
        }
    }

    private async void Upload_Click(object sender, RoutedEventArgs e)
    {
        var picker = new FileOpenPicker();
        picker.FileTypeFilter.Add("*");

        // WinUI3 requires initializing picker with window handle
        var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(App.MainWindow);
        WinRT.Interop.InitializeWithWindow.Initialize(picker, hwnd);

        var files = await picker.PickMultipleFilesAsync();
        if (files is null || _viewModel is null) return;

        await UploadFilesAsync(files.Select(f => f.Path));
    }

    private void Page_DragOver(object sender, DragEventArgs e)
    {
        if (_viewModel?.IsPty == true && e.DataView.Contains(StandardDataFormats.StorageItems))
        {
            e.AcceptedOperation = DataPackageOperation.Copy;
            DragOverlay.Visibility = Visibility.Visible;
        }
    }

    private void Page_DragLeave(object sender, DragEventArgs e)
    {
        DragOverlay.Visibility = Visibility.Collapsed;
    }

    private async void Page_Drop(object sender, DragEventArgs e)
    {
        DragOverlay.Visibility = Visibility.Collapsed;
        if (_viewModel is null) return;

        if (e.DataView.Contains(StandardDataFormats.StorageItems))
        {
            var items = await e.DataView.GetStorageItemsAsync();
            var paths = items.OfType<StorageFile>().Select(f => f.Path);
            await UploadFilesAsync(paths);
        }
    }

    private async Task UploadFilesAsync(IEnumerable<string> filePaths)
    {
        if (_viewModel is null) return;

        // Show transfer panel when uploads start
        TransferPanel.Visibility = Visibility.Visible;

        var tasks = filePaths.Select(async path =>
        {
            try
            {
                await _viewModel.UploadFileAsync(path);
            }
            catch (Exception ex)
            {
                // Surface upload errors to the user instead of swallowing them
                _viewModel.ErrorMessage = $"Upload failed ({System.IO.Path.GetFileName(path)}): {ex.Message}";
            }
        });

        await Task.WhenAll(tasks);
    }

    private async void Restart_Click(object sender, RoutedEventArgs e)
    {
        if (_viewModel is not null)
        {
            await _viewModel.RestartCommand.ExecuteAsync(null);
        }
    }
}
