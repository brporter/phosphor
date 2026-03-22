using System.Text.Json.Serialization;
using Phosphor.Models;

namespace Phosphor;

[JsonSerializable(typeof(SessionData))]
[JsonSerializable(typeof(SessionData[]))]
[JsonSerializable(typeof(AuthUser))]
[JsonSerializable(typeof(JoinPayload))]
[JsonSerializable(typeof(JoinedPayload))]
[JsonSerializable(typeof(ResizePayload))]
[JsonSerializable(typeof(ErrorPayload))]
[JsonSerializable(typeof(ViewerCountPayload))]
[JsonSerializable(typeof(ModePayload))]
[JsonSerializable(typeof(ReconnectPayload))]
[JsonSerializable(typeof(ProcessExitedPayload))]
[JsonSerializable(typeof(FileStartPayload))]
[JsonSerializable(typeof(FileEndPayload))]
[JsonSerializable(typeof(FileAckPayload))]
[JsonSerializable(typeof(AuthConfigResponse))]
[JsonSerializable(typeof(AuthLoginRequest))]
[JsonSerializable(typeof(AuthLoginResponse))]
[JsonSerializable(typeof(AuthPollResponse))]
[JsonSerializable(typeof(CredentialData))]
[JsonSourceGenerationOptions(
    PropertyNamingPolicy = JsonKnownNamingPolicy.SnakeCaseLower,
    DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull)]
public partial class PhosphorJsonContext : JsonSerializerContext;
