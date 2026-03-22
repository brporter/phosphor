// Mirrors internal/protocol/messages.go and web/src/lib/protocol.ts — kept manually in sync.
using System.Text.Json.Serialization;

namespace Phosphor.Models;

public static class MsgType
{
    public const byte Stdout = 0x01;
    public const byte Stdin = 0x02;
    public const byte Resize = 0x03;
    public const byte Hello = 0x10;
    public const byte Welcome = 0x11;
    public const byte Join = 0x12;
    public const byte Joined = 0x13;
    public const byte Reconnect = 0x14;
    public const byte End = 0x15;
    public const byte Error = 0x16;
    public const byte ProcessExited = 0x17;
    public const byte Restart = 0x18;
    public const byte ViewerCount = 0x20;
    public const byte Mode = 0x21;
    public const byte SpawnRequest = 0x22;
    public const byte SpawnComplete = 0x23;
    public const byte Ping = 0x30;
    public const byte Pong = 0x31;
    public const byte FileStart = 0x40;
    public const byte FileChunk = 0x41;
    public const byte FileEnd = 0x42;
    public const byte FileAck = 0x43;
}

public sealed class JoinPayload
{
    [JsonPropertyName("token")]
    public string Token { get; set; } = "";

    [JsonPropertyName("session_id")]
    public string SessionId { get; set; } = "";
}

public sealed class JoinedPayload
{
    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "";

    [JsonPropertyName("cols")]
    public int Cols { get; set; }

    [JsonPropertyName("rows")]
    public int Rows { get; set; }

    [JsonPropertyName("command")]
    public string Command { get; set; } = "";

    [JsonPropertyName("encrypted")]
    public bool Encrypted { get; set; }

    [JsonPropertyName("encryption_salt")]
    public string? EncryptionSalt { get; set; }
}

public sealed class ResizePayload
{
    [JsonPropertyName("cols")]
    public int Cols { get; set; }

    [JsonPropertyName("rows")]
    public int Rows { get; set; }
}

public sealed class ErrorPayload
{
    [JsonPropertyName("code")]
    public string Code { get; set; } = "";

    [JsonPropertyName("message")]
    public string Message { get; set; } = "";
}

public sealed class ViewerCountPayload
{
    [JsonPropertyName("count")]
    public int Count { get; set; }
}

public sealed class ModePayload
{
    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "";
}

public sealed class ReconnectPayload
{
    [JsonPropertyName("status")]
    public string Status { get; set; } = "";
}

public sealed class ProcessExitedPayload
{
    [JsonPropertyName("exit_code")]
    public int ExitCode { get; set; }
}

public sealed class FileStartPayload
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("name")]
    public string Name { get; set; } = "";

    [JsonPropertyName("size")]
    public long Size { get; set; }
}

public sealed class FileEndPayload
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("sha256")]
    public string Sha256 { get; set; } = "";
}

public sealed class FileAckPayload
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("status")]
    public string Status { get; set; } = "";

    [JsonPropertyName("error")]
    public string? Error { get; set; }

    // Go uses int64 with omitempty (zero → omitted); C# nullable maps absent JSON field → null.
    [JsonPropertyName("bytes_written")]
    public long? BytesWritten { get; set; }
}
