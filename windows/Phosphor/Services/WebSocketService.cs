using System.Net.WebSockets;
using System.Text.Json;
using Phosphor.Models;

namespace Phosphor.Services;

/// <summary>
/// Manages a WebSocket connection to the relay for viewing a terminal session.
/// Handles the binary protocol encode/decode and dispatches messages via callbacks.
/// </summary>
public sealed class WebSocketService : IDisposable
{
    private ClientWebSocket? _ws;
    private CancellationTokenSource? _cts;
    private PeriodicTimer? _pingTimer;
    private const int InitialBufferSize = 64 * 1024;  // 64KB initial
    private const int MaxMessageSize = 4 * 1024 * 1024; // 4MB hard limit

    // Callbacks — set by TerminalViewModel before connecting
    public Action<ReadOnlyMemory<byte>>? OnStdout { get; set; }
    public Action<JoinedPayload>? OnJoined { get; set; }
    public Action<ReconnectPayload>? OnReconnect { get; set; }
    public Action<ErrorPayload>? OnError { get; set; }
    public Action<ProcessExitedPayload>? OnProcessExited { get; set; }
    public Action<ViewerCountPayload>? OnViewerCount { get; set; }
    public Action<ModePayload>? OnMode { get; set; }
    public event Action<FileAckPayload>? OnFileAck;
    public Action? OnEnd { get; set; }
    public Action<Exception>? OnDisconnected { get; set; }

    // Track active file transfer IDs for cleanup on disconnect
    private readonly HashSet<string> _activeTransferIds = [];

    /// <summary>
    /// Connect to a session and start the receive loop.
    /// </summary>
    public async Task ConnectAsync(string relayBaseUrl, string sessionId, string token)
    {
        _cts = new CancellationTokenSource();

        var wsUrl = relayBaseUrl
            .Replace("https://", "wss://")
            .Replace("http://", "ws://")
            .TrimEnd('/');

        _ws = new ClientWebSocket();
        _ws.Options.AddSubProtocol("phosphor");

        await _ws.ConnectAsync(new Uri($"{wsUrl}/ws/view/{sessionId}"), _cts.Token);

        // Send Join message
        var join = new JoinPayload { Token = token, SessionId = sessionId };
        var joinBytes = ProtocolCodec.EncodeJson(MsgType.Join, join, PhosphorJsonContext.Default.JoinPayload);
        await _ws.SendAsync(joinBytes, WebSocketMessageType.Binary, true, _cts.Token);

        // Start ping timer
        _pingTimer = new PeriodicTimer(TimeSpan.FromSeconds(30));
        _ = PingLoopAsync(_cts.Token);

        // Start receive loop
        _ = ReceiveLoopAsync(_cts.Token);
    }

    private async Task ReceiveLoopAsync(CancellationToken ct)
    {
        try
        {
            var buffer = new byte[InitialBufferSize];

            while (!ct.IsCancellationRequested && _ws?.State == WebSocketState.Open)
            {
                // Accumulate frames until EndOfMessage, growing buffer as needed
                int totalBytes = 0;
                ValueWebSocketReceiveResult result;
                do
                {
                    if (totalBytes == buffer.Length)
                    {
                        // Buffer full but message not complete — grow or reject
                        if (buffer.Length >= MaxMessageSize)
                        {
                            // Message exceeds hard limit — skip it and close
                            OnDisconnected?.Invoke(
                                new InvalidOperationException($"Message exceeded {MaxMessageSize} byte limit"));
                            return;
                        }
                        var newSize = Math.Min(buffer.Length * 2, MaxMessageSize);
                        var newBuffer = new byte[newSize];
                        buffer.AsSpan(0, totalBytes).CopyTo(newBuffer);
                        buffer = newBuffer;
                    }

                    result = await _ws.ReceiveAsync(
                        buffer.AsMemory(totalBytes), ct);
                    totalBytes += result.Count;
                } while (!result.EndOfMessage);

                if (result.MessageType == WebSocketMessageType.Close)
                {
                    OnEnd?.Invoke();
                    return;
                }

                var data = new ReadOnlyMemory<byte>(buffer, 0, totalBytes);
                var (type, payload) = ProtocolCodec.Decode(data);

                switch (type)
                {
                    case MsgType.Stdout:
                        OnStdout?.Invoke(payload);
                        break;
                    case MsgType.Joined:
                        var joined = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.JoinedPayload);
                        OnJoined?.Invoke(joined);
                        break;
                    case MsgType.Reconnect:
                        var reconnect = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.ReconnectPayload);
                        OnReconnect?.Invoke(reconnect);
                        break;
                    case MsgType.Error:
                        var error = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.ErrorPayload);
                        OnError?.Invoke(error);
                        break;
                    case MsgType.ProcessExited:
                        var exited = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.ProcessExitedPayload);
                        OnProcessExited?.Invoke(exited);
                        break;
                    case MsgType.ViewerCount:
                        var vc = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.ViewerCountPayload);
                        OnViewerCount?.Invoke(vc);
                        break;
                    case MsgType.Mode:
                        var mode = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.ModePayload);
                        OnMode?.Invoke(mode);
                        break;
                    case MsgType.FileAck:
                        var ack = ProtocolCodec.DecodeJson(payload, PhosphorJsonContext.Default.FileAckPayload);
                        if (ack.Status is "complete" or "error")
                            _activeTransferIds.Remove(ack.Id);
                        OnFileAck?.Invoke(ack);
                        break;
                    case MsgType.End:
                        OnEnd?.Invoke();
                        return;
                    case MsgType.Ping:
                        await SendRawAsync(ProtocolCodec.Encode(MsgType.Pong), ct);
                        break;
                    case MsgType.Pong:
                        break; // keepalive response, no action
                }
            }
        }
        catch (OperationCanceledException) { }
        catch (Exception ex)
        {
            OnDisconnected?.Invoke(ex);
        }
    }

    private async Task PingLoopAsync(CancellationToken ct)
    {
        try
        {
            while (_pingTimer is not null && await _pingTimer.WaitForNextTickAsync(ct))
            {
                await SendRawAsync(ProtocolCodec.Encode(MsgType.Ping), ct);
            }
        }
        catch (OperationCanceledException) { }
    }

    public async Task SendStdinAsync(ReadOnlyMemory<byte> data, CancellationToken ct = default)
    {
        var msg = ProtocolCodec.EncodeRaw(MsgType.Stdin, data.Span);
        await SendRawAsync(msg, ct);
    }

    public async Task SendResizeAsync(int cols, int rows, CancellationToken ct = default)
    {
        var payload = new ResizePayload { Cols = cols, Rows = rows };
        var msg = ProtocolCodec.EncodeJson(MsgType.Resize, payload, PhosphorJsonContext.Default.ResizePayload);
        await SendRawAsync(msg, ct);
    }

    public async Task SendRestartAsync(CancellationToken ct = default)
    {
        await SendRawAsync(ProtocolCodec.Encode(MsgType.Restart), ct);
    }

    public async Task SendFileStartAsync(FileStartPayload start, CancellationToken ct = default)
    {
        _activeTransferIds.Add(start.Id);
        var msg = ProtocolCodec.EncodeJson(MsgType.FileStart, start, PhosphorJsonContext.Default.FileStartPayload);
        await SendRawAsync(msg, ct);
    }

    public async Task SendFileChunkAsync(string transferId, ReadOnlyMemory<byte> chunk, CancellationToken ct = default)
    {
        // FileChunk payload: [8-byte ASCII ID][chunk data]
        // Transfer IDs must be exactly 8 alphanumeric characters.
        if (transferId is null)
            throw new ArgumentNullException(nameof(transferId));
        if (transferId.Length != 8)
            throw new ArgumentException("FileChunk transferId must be exactly 8 characters.", nameof(transferId));
        foreach (var c in transferId)
        {
            if (!char.IsLetterOrDigit(c))
                throw new ArgumentException("FileChunk transferId must contain only alphanumeric characters.", nameof(transferId));
        }

        var idBytes = System.Text.Encoding.ASCII.GetBytes(transferId);
        var payload = new byte[idBytes.Length + chunk.Length];
        idBytes.CopyTo(payload, 0);
        chunk.CopyTo(payload.AsMemory(idBytes.Length));
        var msg = ProtocolCodec.EncodeRaw(MsgType.FileChunk, payload);
        await SendRawAsync(msg, ct);
    }

    public async Task SendFileEndAsync(FileEndPayload end, CancellationToken ct = default)
    {
        var msg = ProtocolCodec.EncodeJson(MsgType.FileEnd, end, PhosphorJsonContext.Default.FileEndPayload);
        await SendRawAsync(msg, ct);
    }

    private async Task SendRawAsync(byte[] data, CancellationToken ct)
    {
        if (_ws?.State == WebSocketState.Open)
        {
            await _ws.SendAsync(data, WebSocketMessageType.Binary, true, ct);
        }
    }

    /// <summary>
    /// Send End message and close the WebSocket cleanly.
    /// Aborts any in-flight file transfers first.
    /// </summary>
    public async Task DisconnectAsync()
    {
        if (_ws?.State == WebSocketState.Open)
        {
            try
            {
                // Abort in-flight file transfers
                foreach (var id in _activeTransferIds)
                {
                    var end = new FileEndPayload { Id = id, Sha256 = "" };
                    await SendFileEndAsync(end);
                }
                _activeTransferIds.Clear();

                // Send End message for clean disconnect
                await SendRawAsync(ProtocolCodec.Encode(MsgType.End), CancellationToken.None);
                await _ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "", CancellationToken.None);
            }
            catch { }
        }

        _cts?.Cancel();
    }

    public void Dispose()
    {
        _cts?.Cancel();
        _pingTimer?.Dispose();
        _ws?.Dispose();
        _cts?.Dispose();
    }
}
