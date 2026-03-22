using System.Text.Json.Serialization;

namespace Phosphor.Models;

public sealed class SessionData
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "pty";

    [JsonPropertyName("command")]
    public string Command { get; set; } = "";

    [JsonPropertyName("hostname")]
    public string Hostname { get; set; } = "";

    [JsonPropertyName("cols")]
    public int Cols { get; set; }

    [JsonPropertyName("rows")]
    public int Rows { get; set; }

    [JsonPropertyName("viewer_count")]
    public int ViewerCount { get; set; }

    [JsonPropertyName("encrypted")]
    public bool Encrypted { get; set; }

    [JsonPropertyName("connected")]
    public bool Connected { get; set; }

    [JsonPropertyName("lazy")]
    public bool Lazy { get; set; }

    [JsonPropertyName("exited")]
    public bool Exited { get; set; }

    [JsonPropertyName("exit_code")]
    public int? ExitCode { get; set; }

    public bool IsPty => Mode == "pty";
}
