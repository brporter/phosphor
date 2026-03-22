using System.Security.Cryptography;
using System.Text;

namespace Phosphor.Helpers;

public sealed class CryptoHelper : IDisposable
{
    private const int KeySize = 32;
    private const int NonceSize = 12;
    private const int TagSize = 16;
    private const int Pbkdf2Iterations = 100_000;

    private byte[]? _key;

    /// <summary>
    /// Derive key from passphrase chars and salt.
    /// The caller is responsible for clearing the passphrase char[] after this call.
    /// </summary>
    public void DeriveKey(char[] passphrase, byte[] salt)
    {
        // Convert char[] to byte[] so we can zero it after use
        var passphraseBytes = Encoding.UTF8.GetBytes(passphrase);
        try
        {
            _key = Rfc2898DeriveBytes.Pbkdf2(
                passphraseBytes,
                salt,
                Pbkdf2Iterations,
                HashAlgorithmName.SHA256,
                KeySize);
        }
        finally
        {
            CryptographicOperations.ZeroMemory(passphraseBytes);
        }
    }

    public bool HasKey => _key is not null;

    public byte[] Encrypt(ReadOnlySpan<byte> plaintext)
    {
        if (_key is null) throw new InvalidOperationException("Key not derived");

        var nonce = new byte[NonceSize];
        RandomNumberGenerator.Fill(nonce);

        var ciphertext = new byte[plaintext.Length];
        var tag = new byte[TagSize];

        using var aes = new AesGcm(_key, TagSize);
        aes.Encrypt(nonce, plaintext, ciphertext, tag);

        var result = new byte[NonceSize + ciphertext.Length + TagSize];
        nonce.CopyTo(result, 0);
        ciphertext.CopyTo(result, NonceSize);
        tag.CopyTo(result, NonceSize + ciphertext.Length);
        return result;
    }

    public byte[] Decrypt(ReadOnlySpan<byte> data)
    {
        if (_key is null) throw new InvalidOperationException("Key not derived");
        if (data.Length < NonceSize + TagSize)
            throw new ArgumentException("Data too short for GCM decryption");

        var nonce = data[..NonceSize];
        var ciphertext = data[NonceSize..^TagSize];
        var tag = data[^TagSize..];

        var plaintext = new byte[ciphertext.Length];

        using var aes = new AesGcm(_key, TagSize);
        aes.Decrypt(nonce, ciphertext, tag, plaintext);
        return plaintext;
    }

    public void Dispose()
    {
        if (_key is not null)
        {
            CryptographicOperations.ZeroMemory(_key);
            _key = null;
        }
    }
}
