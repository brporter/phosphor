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
    private readonly object _encryptedBufferLock = new();
    private string? _encryptionSalt;
    private readonly Microsoft.UI.Dispatching.DispatcherQueue _dispatcher =
        Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread();

    private const long MaxUploadSize = 100 * 1024 * 1024; // 100MB relay limit

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
                    _dispatcher.TryEnqueue(() =>
                    {
                        NeedsPassphrase = true;
                        ErrorMessage = "Decryption failed — wrong passphrase?";
                    });
                }
            }
            else if (IsEncrypted && (_crypto is null || !_crypto.HasKey))
            {
                lock (_encryptedBufferLock)
                {
                    _encryptedBuffer.Add(data.ToArray());
                }
            }
            else
            {
                WriteToTerminal?.Invoke(data);
            }
        };

        _ws.OnJoined = joined =>
        {
            _dispatcher.TryEnqueue(() =>
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
            });
        };

        _ws.OnReconnect = r =>
        {
            _dispatcher.TryEnqueue(() => CliConnected = r.Status == "reconnected");
        };

        _ws.OnError = e =>
        {
            _dispatcher.TryEnqueue(() =>
            {
                ErrorMessage = $"[{e.Code}] {e.Message}";
                ConnectionState = "error";
            });
        };

        _ws.OnProcessExited = e =>
        {
            _dispatcher.TryEnqueue(() =>
            {
                ExitCode = e.ExitCode;
                OnPropertyChanged(nameof(HasExited));
            });
        };

        _ws.OnViewerCount = vc =>
        {
            _dispatcher.TryEnqueue(() => ViewerCount = vc.Count);
        };

        _ws.OnMode = m =>
        {
            _dispatcher.TryEnqueue(() =>
            {
                Mode = m.Mode;
                OnPropertyChanged(nameof(IsPty));
            });
        };

        _ws.OnFileAck += ack =>
        {
            _dispatcher.TryEnqueue(() =>
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
                    // Auto-remove after 10 seconds
                    _ = Task.Delay(TimeSpan.FromSeconds(10)).ContinueWith(_ =>
                    {
                        _dispatcher.TryEnqueue(() => Transfers.Remove(transfer));
                    });
                }
            });
        };

        _ws.OnEnd = () =>
        {
            _dispatcher.TryEnqueue(() => ConnectionState = "disconnected");
        };

        _ws.OnDisconnected = ex =>
        {
            _dispatcher.TryEnqueue(() =>
            {
                ConnectionState = "disconnected";
                ErrorMessage = $"Connection lost: {ex.Message}";
            });
        };
    }

    public async Task ConnectAsync(string sessionId)
    {
        // Reset crypto state from any previous session
        _crypto?.Dispose();
        _crypto = null;
        _encryptionSalt = null;
        lock (_encryptedBufferLock)
        {
            _encryptedBuffer.Clear();
        }

        ConnectionState = "connecting";
        ErrorMessage = null;
        ExitCode = null;
        NeedsPassphrase = false;
        IsEncrypted = false;

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
        // Convert passphrase to char[] so we can zero it after use
        var passphraseChars = passphrase.ToCharArray();
        try
        {
            DeriveKey(passphraseChars, _encryptionSalt);
        }
        finally
        {
            Array.Clear(passphraseChars);
        }

        NeedsPassphrase = false;
        ErrorMessage = null;

        // Flush buffered messages
        List<ReadOnlyMemory<byte>> bufferedData;
        lock (_encryptedBufferLock)
        {
            bufferedData = new List<ReadOnlyMemory<byte>>(_encryptedBuffer);
            _encryptedBuffer.Clear();
        }

        foreach (var data in bufferedData)
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
    }

    public void DeriveKey(char[] passphrase, string saltBase64)
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

        // Pre-validate file size against relay limit
        if (fileInfo.Length > MaxUploadSize)
        {
            var transfer = new FileTransfer(
                GenerateTransferId(), fileInfo.Name, fileInfo.Length)
            {
                Status = "error",
                Error = $"File too large ({fileInfo.Length / (1024 * 1024)}MB). Maximum is {MaxUploadSize / (1024 * 1024)}MB."
            };
            Transfers.Add(transfer);
            return;
        }
        if (fileInfo.Length <= 0)
        {
            var transfer = new FileTransfer(
                GenerateTransferId(), fileInfo.Name, fileInfo.Length)
            {
                Status = "error",
                Error = "File is empty."
            };
            Transfers.Add(transfer);
            return;
        }

        var id = GenerateTransferId();
        var transferObj = new FileTransfer(id, fileInfo.Name, fileInfo.Length);
        Transfers.Add(transferObj);

        // Subscribe to ack events BEFORE sending FileStart to avoid missing a fast response.
        // This handler covers both the "accepted" ack and the final "complete"/"error" ack.
        var acceptTcs = new TaskCompletionSource<bool>();
        var completeTcs = new TaskCompletionSource<FileAckPayload>();
        void OnAck(FileAckPayload ack)
        {
            if (ack.Id != id) return;
            if (ack.Status == "accepted")
                acceptTcs.TrySetResult(true);
            else if (ack.Status == "error")
            {
                acceptTcs.TrySetException(new InvalidOperationException(ack.Error ?? "Upload rejected"));
                completeTcs.TrySetResult(ack);
            }
            else if (ack.Status == "complete")
                completeTcs.TrySetResult(ack);
        }
        _ws.OnFileAck += OnAck;

        try
        {
            // Send FileStart
            await _ws.SendFileStartAsync(new FileStartPayload
            {
                Id = id,
                Name = fileInfo.Name,
                Size = fileInfo.Length,
            }, ct);

            transferObj.Status = "pending";

            // Wait for FileAck "accepted" (30s timeout)
            using (var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(ct))
            {
                timeoutCts.CancelAfter(TimeSpan.FromSeconds(30));
                timeoutCts.Token.Register(() => acceptTcs.TrySetCanceled());
                await acceptTcs.Task;
            }

            // Stream chunks off the UI thread to avoid jank during large uploads
            await Task.Run(async () =>
            {
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
            }, ct);

            // Wait for the final complete/error FileAck (30s timeout)
            using (var finalCts = CancellationTokenSource.CreateLinkedTokenSource(ct))
            {
                finalCts.CancelAfter(TimeSpan.FromSeconds(30));
                finalCts.Token.Register(() => completeTcs.TrySetCanceled());
                await completeTcs.Task;
            }
        }
        finally
        {
            _ws.OnFileAck -= OnAck;
        }
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
        _crypto = null;
        _encryptionSalt = null;
        lock (_encryptedBufferLock)
        {
            _encryptedBuffer.Clear();
        }
    }

    public void Dispose()
    {
        _crypto?.Dispose();
        _ws.Dispose();
    }
}
