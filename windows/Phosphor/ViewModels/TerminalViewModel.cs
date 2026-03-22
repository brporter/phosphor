using System.Collections.ObjectModel;
using System.Security.Cryptography;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Phosphor.Helpers;
using Phosphor.Models;
using Phosphor.Services;

namespace Phosphor.ViewModels;

public sealed partial class TerminalViewModel : ObservableObject, IDisposable
{
    private readonly WebSocketService _ws;
    private readonly ApiClient _api;
    private CryptoHelper? _crypto;
    private readonly List<ReadOnlyMemory<byte>> _encryptedBuffer = [];
    private string? _encryptionSalt;
    private readonly Microsoft.UI.Dispatching.DispatcherQueue _dispatcher =
        Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread();

    [ObservableProperty]
    public partial string ConnectionState { get; set; } = "connecting"; // connecting, connected, disconnected, error

    [ObservableProperty]
    public partial string Command { get; set; } = "";

    [ObservableProperty]
    public partial int ViewerCount { get; set; }

    [ObservableProperty]
    public partial string Mode { get; set; } = "pty";

    [ObservableProperty]
    public partial bool IsEncrypted { get; set; }

    [ObservableProperty]
    public partial bool NeedsPassphrase { get; set; }

    [ObservableProperty]
    public partial int? ExitCode { get; set; }

    [ObservableProperty]
    public partial string? ErrorMessage { get; set; }

    [ObservableProperty]
    public partial bool CliConnected { get; set; } = true;

    public bool IsPty => Mode == "pty";
    public bool HasExited => ExitCode.HasValue;

    public ObservableCollection<FileTransfer> Transfers { get; } = [];

    // Callbacks to terminal control
    public Action<ReadOnlyMemory<byte>>? WriteToTerminal { get; set; }
    public Action<int, int>? SetTerminalSize { get; set; }

    public TerminalViewModel(ApiClient api)
    {
        _api = api;
        _ws = new WebSocketService();
        SetupCallbacks();
    }

    private void SetupCallbacks()
    {
        _ws.OnStdout = data =>
        {
            if (IsEncrypted && _crypto is not null && _crypto.HasKey)
            {
                try
                {
                    var decrypted = _crypto.Decrypt(data.Span);
                    WriteToTerminal?.Invoke(decrypted);
                }
                catch (CryptographicException)
                {
                    // Wrong passphrase — clear key and re-prompt
                    _crypto.Dispose();
                    _crypto = new CryptoHelper();
                    NeedsPassphrase = true;
                    ErrorMessage = "Decryption failed — wrong passphrase?";
                }
            }
            else if (IsEncrypted && (_crypto is null || !_crypto.HasKey))
            {
                _encryptedBuffer.Add(data.ToArray());
            }
            else
            {
                WriteToTerminal?.Invoke(data);
            }
        };

        _ws.OnJoined = joined =>
        {
            Mode = joined.Mode;
            Command = joined.Command;
            IsEncrypted = joined.Encrypted;
            ConnectionState = "connected";
            SetTerminalSize?.Invoke(joined.Cols, joined.Rows);

            if (joined.Encrypted && !string.IsNullOrEmpty(joined.EncryptionSalt))
            {
                _crypto = new CryptoHelper();
                _encryptionSalt = joined.EncryptionSalt;
                NeedsPassphrase = true;
            }

            OnPropertyChanged(nameof(IsPty));
        };

        _ws.OnReconnect = r =>
        {
            CliConnected = r.Status == "reconnected";
        };

        _ws.OnError = e =>
        {
            ErrorMessage = $"[{e.Code}] {e.Message}";
            ConnectionState = "error";
        };

        _ws.OnProcessExited = e =>
        {
            ExitCode = e.ExitCode;
            OnPropertyChanged(nameof(HasExited));
        };

        _ws.OnViewerCount = vc => ViewerCount = vc.Count;
        _ws.OnMode = m =>
        {
            Mode = m.Mode;
            OnPropertyChanged(nameof(IsPty));
        };

        _ws.OnFileAck += ack =>
        {
            var transfer = Transfers.FirstOrDefault(t => t.Id == ack.Id);
            if (transfer is null) return;

            transfer.Status = ack.Status;
            if (ack.BytesWritten.HasValue)
                transfer.BytesWritten = ack.BytesWritten.Value;
            if (ack.Error is not null)
                transfer.Error = ack.Error;

            if (ack.Status is "complete" or "error")
            {
                // Auto-remove after 10 seconds, dispatched to UI thread
                _ = Task.Delay(TimeSpan.FromSeconds(10)).ContinueWith(_ =>
                {
                    _dispatcher.TryEnqueue(() => Transfers.Remove(transfer));
                });
            }
        };

        _ws.OnEnd = () =>
        {
            ConnectionState = "disconnected";
        };

        _ws.OnDisconnected = ex =>
        {
            ConnectionState = "disconnected";
            ErrorMessage = $"Connection lost: {ex.Message}";
        };
    }

    public async Task ConnectAsync(string sessionId)
    {
        ConnectionState = "connecting";
        ErrorMessage = null;
        ExitCode = null;

        try
        {
            await _ws.ConnectAsync(_api.BaseUrl, sessionId, _api.Token);
        }
        catch (Exception ex)
        {
            ConnectionState = "error";
            ErrorMessage = $"Connection failed: {ex.Message}";
        }
    }

    /// <summary>
    /// Submit passphrase for encrypted session.
    /// </summary>
    [RelayCommand]
    private void SubmitPassphrase(string passphrase)
    {
        if (_crypto is null || string.IsNullOrEmpty(passphrase) || _encryptionSalt is null) return;

        // Derive key from passphrase + salt stored from Joined message
        DeriveKey(passphrase, _encryptionSalt);
        NeedsPassphrase = false;
        ErrorMessage = null;

        // Flush buffered messages
        foreach (var data in _encryptedBuffer)
        {
            try
            {
                var decrypted = _crypto.Decrypt(data.Span);
                WriteToTerminal?.Invoke(decrypted);
            }
            catch (CryptographicException)
            {
                _crypto.Dispose();
                _crypto = new CryptoHelper();
                NeedsPassphrase = true;
                ErrorMessage = "Decryption failed — wrong passphrase?";
                return;
            }
        }
        _encryptedBuffer.Clear();
    }

    public void DeriveKey(string passphrase, string saltBase64)
    {
        var salt = Convert.FromBase64String(saltBase64);
        _crypto?.DeriveKey(passphrase, salt);
    }

    public async Task SendInputAsync(ReadOnlyMemory<byte> data)
    {
        if (!IsPty) return;

        if (IsEncrypted && _crypto is not null && _crypto.HasKey)
        {
            var encrypted = _crypto.Encrypt(data.Span);
            await _ws.SendStdinAsync(encrypted);
        }
        else if (!IsEncrypted)
        {
            await _ws.SendStdinAsync(data);
        }
    }

    public async Task SendResizeAsync(int cols, int rows)
    {
        await _ws.SendResizeAsync(cols, rows);
    }

    [RelayCommand]
    private async Task RestartAsync()
    {
        ExitCode = null;
        OnPropertyChanged(nameof(HasExited));
        await _ws.SendRestartAsync();
    }

    /// <summary>
    /// Upload a file to the CLI via the relay.
    /// </summary>
    public async Task UploadFileAsync(string filePath, CancellationToken ct = default)
    {
        var fileInfo = new FileInfo(filePath);
        var id = GenerateTransferId();
        var transfer = new FileTransfer(id, fileInfo.Name, fileInfo.Length);
        Transfers.Add(transfer);

        // Send FileStart
        await _ws.SendFileStartAsync(new FileStartPayload
        {
            Id = id,
            Name = fileInfo.Name,
            Size = fileInfo.Length,
        }, ct);

        transfer.Status = "pending";

        // Wait for FileAck "accepted" (30s timeout)
        var acceptTcs = new TaskCompletionSource<bool>();
        void OnAck(FileAckPayload ack)
        {
            if (ack.Id == id && ack.Status == "accepted")
                acceptTcs.TrySetResult(true);
            else if (ack.Id == id && ack.Status == "error")
                acceptTcs.TrySetException(new InvalidOperationException(ack.Error ?? "Upload rejected"));
        }
        _ws.OnFileAck += OnAck;
        try
        {
            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            timeoutCts.CancelAfter(TimeSpan.FromSeconds(30));
            timeoutCts.Token.Register(() => acceptTcs.TrySetCanceled());
            await acceptTcs.Task;
        }
        finally
        {
            _ws.OnFileAck -= OnAck;
        }

        // Stream chunks
        const int chunkSize = 32 * 1024;
        using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
        await using var stream = File.OpenRead(filePath);
        var buffer = new byte[chunkSize];

        int bytesRead;
        while ((bytesRead = await stream.ReadAsync(buffer.AsMemory(0, chunkSize), ct)) > 0)
        {
            var chunk = buffer.AsMemory(0, bytesRead);
            hash.AppendData(chunk.Span);
            await _ws.SendFileChunkAsync(id, chunk, ct);
        }

        // Send FileEnd with SHA256
        var sha256 = Convert.ToHexStringLower(hash.GetHashAndReset());
        await _ws.SendFileEndAsync(new FileEndPayload { Id = id, Sha256 = sha256 }, ct);
    }

    private static string GenerateTransferId()
    {
        const string chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
        Span<byte> bytes = stackalloc byte[8];
        RandomNumberGenerator.Fill(bytes);
        return string.Create(8, bytes.ToArray(), (span, b) =>
        {
            for (int i = 0; i < span.Length; i++)
                span[i] = chars[b[i] % chars.Length];
        });
    }

    public async Task DisconnectAsync()
    {
        await _ws.DisconnectAsync();
        _crypto?.Dispose();
        _encryptedBuffer.Clear();
    }

    public void Dispose()
    {
        _crypto?.Dispose();
        _ws.Dispose();
    }
}
