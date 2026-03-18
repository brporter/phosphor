import Foundation

/// Binary protocol codec mirroring internal/protocol/codec.go
enum ProtocolCodec {

    enum CodecError: Error {
        case emptyMessage
        case unknownType(UInt8)
        case encodingFailed
    }

    // MARK: - Encode

    /// Encode a message with raw bytes payload (for Stdout/Stdin).
    static func encode(type: MessageType, payload: Data) -> Data {
        var data = Data(capacity: 1 + payload.count)
        data.append(type.rawValue)
        data.append(payload)
        return data
    }

    /// Encode a message with no payload (for Ping/Pong/End).
    static func encode(type: MessageType) -> Data {
        return Data([type.rawValue])
    }

    /// Encode a message with a JSON-encodable payload (for control messages).
    static func encode<T: Encodable>(type: MessageType, json: T) -> Data {
        let encoder = JSONEncoder()
        guard let jsonData = try? encoder.encode(json) else {
            return Data([type.rawValue])
        }
        var data = Data(capacity: 1 + jsonData.count)
        data.append(type.rawValue)
        data.append(jsonData)
        return data
    }

    // MARK: - Decode

    /// Decode a binary message into type and raw payload.
    static func decode(_ data: Data) throws -> (MessageType, Data) {
        guard !data.isEmpty else {
            throw CodecError.emptyMessage
        }

        guard let type = MessageType(rawValue: data[data.startIndex]) else {
            throw CodecError.unknownType(data[data.startIndex])
        }

        let payload = data.dropFirst()
        return (type, Data(payload))
    }

    /// Decode a JSON payload into a Decodable type.
    static func decodeJSON<T: Decodable>(_ payload: Data) throws -> T {
        return try JSONDecoder().decode(T.self, from: payload)
    }
}

// MARK: - Payload types

struct JoinPayload: Codable {
    let token: String
    let sessionId: String

    enum CodingKeys: String, CodingKey {
        case token
        case sessionId = "session_id"
    }
}

struct JoinedPayload: Codable {
    let mode: String
    let cols: Int
    let rows: Int
    let command: String
}

struct ResizePayload: Codable {
    let cols: Int
    let rows: Int
}

struct ErrorPayload: Codable {
    let code: String
    let message: String
}

struct ReconnectPayload: Codable {
    let status: String
}

struct ViewerCountPayload: Codable {
    let count: Int
}

struct ModePayload: Codable {
    let mode: String
}

struct ProcessExitedPayload: Codable {
    let exitCode: Int

    enum CodingKeys: String, CodingKey {
        case exitCode = "exit_code"
    }
}

struct FileStartPayload: Codable {
    let id: String
    let name: String
    let size: Int
}

struct FileEndPayload: Codable {
    let id: String
    let sha256: String
}

struct FileAckPayload: Codable {
    let id: String
    let status: String  // "accepted", "progress", "complete", "error"
    let error: String?
    let bytesWritten: Int?

    enum CodingKeys: String, CodingKey {
        case id, status, error
        case bytesWritten = "bytes_written"
    }
}
