using System.Text.Json;
using Phosphor.Models;
using Windows.Security.Credentials;

namespace Phosphor.Services;

public sealed class CredentialStore
{
    private const string ResourcePrefix = "phosphor/";
    private readonly PasswordVault _vault = new();

    public void Save(string relayHost, string idToken, string relayUrl)
    {
        var data = new CredentialData { IdToken = idToken, RelayUrl = relayUrl };
        var json = JsonSerializer.Serialize(data, PhosphorJsonContext.Default.CredentialData);
        var resource = ResourcePrefix + relayHost;

        TryRemove(relayHost);

        _vault.Add(new PasswordCredential(resource, "user", json));
    }

    public CredentialData? Load(string relayHost)
    {
        try
        {
            var resource = ResourcePrefix + relayHost;
            var credential = _vault.Retrieve(resource, "user");
            credential.RetrievePassword();
            return JsonSerializer.Deserialize(credential.Password, PhosphorJsonContext.Default.CredentialData);
        }
        catch
        {
            return null;
        }
    }

    public void TryRemove(string relayHost)
    {
        try
        {
            var resource = ResourcePrefix + relayHost;
            var credential = _vault.Retrieve(resource, "user");
            _vault.Remove(credential);
        }
        catch
        {
        }
    }
}
