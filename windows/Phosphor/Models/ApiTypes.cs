using System.Text.Json.Serialization;

namespace Phosphor.Models;

public sealed class AuthConfigResponse
{
    [JsonPropertyName("providers")]
    public string[] Providers { get; set; } = [];
}

public sealed class AuthLoginRequest
{
    [JsonPropertyName("provider")]
    public string Provider { get; set; } = "";

    [JsonPropertyName("source")]
    public string Source { get; set; } = "mobile";
}

public sealed class AuthLoginResponse
{
    [JsonPropertyName("session_id")]
    public string SessionId { get; set; } = "";

    [JsonPropertyName("auth_url")]
    public string AuthUrl { get; set; } = "";
}

public sealed class AuthPollResponse
{
    [JsonPropertyName("status")]
    public string Status { get; set; } = "";

    [JsonPropertyName("id_token")]
    public string? IdToken { get; set; }
}

public sealed class CredentialData
{
    [JsonPropertyName("id_token")]
    public string IdToken { get; set; } = "";

    [JsonPropertyName("relay_url")]
    public string RelayUrl { get; set; } = "";
}
