using System.Net.Http.Headers;
using System.Text.Json;
using Phosphor.Models;

namespace Phosphor.Services;

public sealed class ApiClient : IDisposable
{
    private readonly HttpClient _http = new();
    private string _baseUrl = "";
    private string _token = "";

    public void Configure(string baseUrl, string token)
    {
        _baseUrl = baseUrl.TrimEnd('/');
        _token = token;

        if (!string.IsNullOrEmpty(token))
        {
            _http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
        }
        else
        {
            _http.DefaultRequestHeaders.Authorization = null;
        }
    }

    public string BaseUrl => _baseUrl;
    public string Token => _token;

    public async Task<AuthConfigResponse> GetAuthConfigAsync(CancellationToken ct = default)
    {
        var res = await _http.GetAsync($"{_baseUrl}/api/auth/config", ct);
        res.EnsureSuccessStatusCode();
        var stream = await res.Content.ReadAsStreamAsync(ct);
        return await JsonSerializer.DeserializeAsync(stream, PhosphorJsonContext.Default.AuthConfigResponse, ct)
            ?? throw new InvalidOperationException("Empty auth config response");
    }

    public async Task<AuthLoginResponse> LoginAsync(string provider, CancellationToken ct = default)
    {
        // Use "desktop" source to trigger the phosphor:// custom scheme redirect flow.
        // The relay treats "desktop" same as "mobile" for redirect purposes.
        var request = new AuthLoginRequest { Provider = provider, Source = "desktop" };
        var json = JsonSerializer.SerializeToUtf8Bytes(request, PhosphorJsonContext.Default.AuthLoginRequest);
        var content = new ByteArrayContent(json);
        content.Headers.ContentType = new("application/json");

        var res = await _http.PostAsync($"{_baseUrl}/api/auth/login", content, ct);
        res.EnsureSuccessStatusCode();
        var stream = await res.Content.ReadAsStreamAsync(ct);
        return await JsonSerializer.DeserializeAsync(stream, PhosphorJsonContext.Default.AuthLoginResponse, ct)
            ?? throw new InvalidOperationException("Empty login response");
    }

    public async Task<AuthPollResponse> PollAuthAsync(string sessionId, CancellationToken ct = default)
    {
        var res = await _http.GetAsync($"{_baseUrl}/api/auth/poll?session={Uri.EscapeDataString(sessionId)}", ct);
        res.EnsureSuccessStatusCode();
        var stream = await res.Content.ReadAsStreamAsync(ct);
        return await JsonSerializer.DeserializeAsync(stream, PhosphorJsonContext.Default.AuthPollResponse, ct)
            ?? throw new InvalidOperationException("Empty poll response");
    }

    public async Task<SessionData[]> GetSessionsAsync(CancellationToken ct = default)
    {
        var res = await _http.GetAsync($"{_baseUrl}/api/sessions", ct);
        res.EnsureSuccessStatusCode();
        var stream = await res.Content.ReadAsStreamAsync(ct);
        return await JsonSerializer.DeserializeAsync(stream, PhosphorJsonContext.Default.SessionDataArray, ct)
            ?? [];
    }

    public async Task DestroySessionAsync(string sessionId, CancellationToken ct = default)
    {
        var res = await _http.DeleteAsync($"{_baseUrl}/api/sessions/{Uri.EscapeDataString(sessionId)}", ct);
        res.EnsureSuccessStatusCode();
    }

    public void Dispose() => _http.Dispose();
}
