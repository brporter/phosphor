using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization.Metadata;
using Phosphor.Models;

namespace Phosphor.Services;

public static class ProtocolCodec
{
    public static byte[] Encode(byte type)
    {
        return [type];
    }

    public static byte[] EncodeRaw(byte type, ReadOnlySpan<byte> payload)
    {
        var buf = new byte[1 + payload.Length];
        buf[0] = type;
        payload.CopyTo(buf.AsSpan(1));
        return buf;
    }

    public static byte[] EncodeJson<T>(byte type, T payload, JsonTypeInfo<T> typeInfo)
    {
        var json = JsonSerializer.SerializeToUtf8Bytes(payload, typeInfo);
        var buf = new byte[1 + json.Length];
        buf[0] = type;
        json.CopyTo(buf, 1);
        return buf;
    }

    public static (byte Type, ReadOnlyMemory<byte> Payload) Decode(ReadOnlyMemory<byte> data)
    {
        if (data.Length == 0)
            throw new ArgumentException("Empty message");

        var type = data.Span[0];
        var payload = data.Length > 1 ? data[1..] : ReadOnlyMemory<byte>.Empty;
        return (type, payload);
    }

    public static T DecodeJson<T>(ReadOnlyMemory<byte> payload, JsonTypeInfo<T> typeInfo)
    {
        return JsonSerializer.Deserialize(payload.Span, typeInfo)
            ?? throw new InvalidOperationException($"Failed to deserialize {typeof(T).Name}");
    }
}
