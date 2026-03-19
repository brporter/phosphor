import Foundation
import CryptoKit

/// Events emitted by the WebSocket connection.
enum WebSocketEvent {
    case joined(JoinedPayload)
    case stdout(Data)
    case resize(Int, Int)
    case reconnect(String)  // "disconnected" or "reconnected"
    case viewerCount(Int)
    case mode(String)
    case end
    case error(String)
    case processExited(Int)
    case restart
    case disconnected
    case fileAck(FileAckPayload)
    case encryptionRequired(salt: String)
    case decryptionFailed(rawData: Data)
}

/// Manages a WebSocket connection to the relay for viewing a session.
final class WebSocketManager: NSObject, @unchecked Sendable {

    private var webSocketTask: URLSessionWebSocketTask?
    private var session: URLSession?
    private var continuation: AsyncStream<WebSocketEvent>.Continuation?
    private var encryptionKey: SymmetricKey?
    private var isEncrypted = false
    private var bufferedEncryptedStdout: [Data] = []

    /// Connect to the relay and return a stream of events.
    func connect(baseURL: String, sessionId: String, token: String) -> AsyncStream<WebSocketEvent> {
        disconnect()

        let wsURL: String
        if baseURL.hasPrefix("https://") {
            wsURL = "wss://" + baseURL.dropFirst("https://".count) + "/ws/view/\(sessionId)"
        } else if baseURL.hasPrefix("http://") {
            wsURL = "ws://" + baseURL.dropFirst("http://".count) + "/ws/view/\(sessionId)"
        } else {
            wsURL = "wss://\(baseURL)/ws/view/\(sessionId)"
        }

        guard let url = URL(string: wsURL) else {
            return AsyncStream { $0.yield(.error("Invalid WebSocket URL")); $0.finish() }
        }

        var request = URLRequest(url: url)
        request.setValue("phosphor", forHTTPHeaderField: "Sec-WebSocket-Protocol")

        let config = URLSessionConfiguration.default
        self.session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
        let task = self.session!.webSocketTask(with: request)
        self.webSocketTask = task

        let stream = AsyncStream<WebSocketEvent> { continuation in
            self.continuation = continuation
            continuation.onTermination = { @Sendable _ in
                task.cancel(with: .goingAway, reason: nil)
            }
        }

        task.resume()

        // Send Join message after connection
        let joinPayload = JoinPayload(token: token, sessionId: sessionId)
        let joinData = ProtocolCodec.encode(type: .join, json: joinPayload)
        task.send(.data(joinData)) { _ in }

        // Start receive loop
        Task { [weak self] in
            await self?.receiveLoop()
        }

        return stream
    }

    func sendStdin(_ data: Data) {
        let payload: Data
        if isEncrypted, let key = encryptionKey {
            guard let encrypted = try? CryptoManager.encrypt(key: key, plaintext: data) else { return }
            payload = encrypted
        } else {
            payload = data
        }
        let message = ProtocolCodec.encode(type: .stdin, payload: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func sendResize(cols: Int, rows: Int) {
        let payload = ResizePayload(cols: cols, rows: rows)
        let message = ProtocolCodec.encode(type: .resize, json: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func sendRestart() {
        let message = ProtocolCodec.encode(type: .restart)
        webSocketTask?.send(.data(message)) { _ in }
    }

    /// Set the derived encryption key and flush buffered stdout.
    /// Returns false if any buffered chunk fails to decrypt (wrong key).
    func setEncryptionKey(_ key: SymmetricKey) -> Bool {
        // Trial-decrypt first buffered chunk to validate key
        if let firstChunk = bufferedEncryptedStdout.first {
            do {
                _ = try CryptoManager.decrypt(key: key, data: firstChunk)
            } catch {
                return false
            }
        }

        self.encryptionKey = key

        // Flush buffered stdout
        for chunk in bufferedEncryptedStdout {
            if let decrypted = try? CryptoManager.decrypt(key: key, data: chunk) {
                continuation?.yield(.stdout(decrypted))
            }
        }
        bufferedEncryptedStdout.removeAll()
        return true
    }

    func clearEncryptionKey() {
        encryptionKey = nil
    }

    func reBufferChunk(_ data: Data) {
        bufferedEncryptedStdout.append(data)
    }

    func sendFileStart(id: String, name: String, size: Int) {
        let payload = FileStartPayload(id: id, name: name, size: size)
        let message = ProtocolCodec.encode(type: .fileStart, json: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func sendFileChunk(id: String, chunk: Data) {
        // FileChunk payload: [8-byte ASCII ID][raw data]
        let idData = Data(id.utf8)
        var payload = Data(capacity: idData.count + chunk.count)
        payload.append(idData)
        payload.append(chunk)
        let message = ProtocolCodec.encode(type: .fileChunk, payload: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func sendFileEnd(id: String, sha256: String) {
        let payload = FileEndPayload(id: id, sha256: sha256)
        let message = ProtocolCodec.encode(type: .fileEnd, json: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func disconnect() {
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil
        session?.invalidateAndCancel()
        session = nil
        continuation?.finish()
        continuation = nil
        encryptionKey = nil
        isEncrypted = false
        bufferedEncryptedStdout.removeAll()
    }

    // MARK: - Private

    private func receiveLoop() async {
        guard let task = webSocketTask else { return }

        while task.state == .running {
            do {
                let message = try await task.receive()
                switch message {
                case .data(let data):
                    handleBinaryMessage(data)
                case .string(let text):
                    if let data = text.data(using: .utf8) {
                        handleBinaryMessage(data)
                    }
                @unknown default:
                    break
                }
            } catch {
                continuation?.yield(.disconnected)
                break
            }
        }
    }

    private func handleBinaryMessage(_ data: Data) {
        guard let (type, payload) = try? ProtocolCodec.decode(data) else { return }

        switch type {
        case .joined:
            if let info: JoinedPayload = try? ProtocolCodec.decodeJSON(payload) {
                if info.encrypted == true, let salt = info.encryptionSalt {
                    isEncrypted = true
                    continuation?.yield(.encryptionRequired(salt: salt))
                }
                continuation?.yield(.joined(info))
            }
        case .stdout:
            if isEncrypted {
                if let key = encryptionKey {
                    do {
                        let decrypted = try CryptoManager.decrypt(key: key, data: payload)
                        continuation?.yield(.stdout(decrypted))
                    } catch {
                        continuation?.yield(.decryptionFailed(rawData: payload))
                    }
                } else {
                    bufferedEncryptedStdout.append(payload)
                }
            } else {
                continuation?.yield(.stdout(payload))
            }
        case .resize:
            if let sz: ResizePayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.resize(sz.cols, sz.rows))
            }
        case .reconnect:
            if let info: ReconnectPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.reconnect(info.status))
            }
        case .viewerCount:
            if let info: ViewerCountPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.viewerCount(info.count))
            }
        case .mode:
            if let info: ModePayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.mode(info.mode))
            }
        case .end:
            continuation?.yield(.end)
        case .error:
            if let err: ErrorPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.error("\(err.code): \(err.message)"))
            }
        case .processExited:
            if let info: ProcessExitedPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.processExited(info.exitCode))
            }
        case .restart:
            continuation?.yield(.restart)
        case .fileAck:
            if let ack: FileAckPayload = try? ProtocolCodec.decodeJSON(payload) {
                continuation?.yield(.fileAck(ack))
            }
        case .ping:
            let pong = ProtocolCodec.encode(type: .pong)
            webSocketTask?.send(.data(pong)) { _ in }
        default:
            break
        }
    }
}

extension WebSocketManager: URLSessionWebSocketDelegate {
    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        // Connection opened — Join message already sent in connect()
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
        reason: Data?
    ) {
        continuation?.yield(.disconnected)
        continuation?.finish()
    }
}
