using System.Text.Json;
using System.Text.Json.Serialization;

namespace Phosphor.Models;

public sealed class AuthUser
{
    [JsonPropertyName("sub")]
    public string Subject { get; set; } = "";

    [JsonPropertyName("email")]
    public string Email { get; set; } = "";

    [JsonPropertyName("iss")]
    public string Issuer { get; set; } = "";

    [JsonPropertyName("exp")]
    public long Exp { get; set; }

    [JsonIgnore]
    public string Token { get; set; } = "";

    [JsonIgnore]
    public bool IsExpired => DateTimeOffset.UtcNow.ToUnixTimeSeconds() >= Exp;

    public static AuthUser FromJwt(string token)
    {
        var parts = token.Split('.');
        if (parts.Length < 2)
            throw new ArgumentException("Invalid JWT format");

        var payload = parts[1];
        payload = payload.Replace('-', '+').Replace('_', '/');
        switch (payload.Length % 4)
        {
            case 2: payload += "=="; break;
            case 3: payload += "="; break;
        }

        var json = Convert.FromBase64String(payload);
        var user = JsonSerializer.Deserialize(json, PhosphorJsonContext.Default.AuthUser)
            ?? throw new InvalidOperationException("Failed to parse JWT payload");
        user.Token = token;
        return user;
    }
}
