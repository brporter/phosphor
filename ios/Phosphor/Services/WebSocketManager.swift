import Foundation

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
}

/// Manages a WebSocket connection to the relay for viewing a session.
final class WebSocketManager: NSObject, @unchecked Sendable {

    private var webSocketTask: URLSessionWebSocketTask?
    private var session: URLSession?
    private var continuation: AsyncStream<WebSocketEvent>.Continuation?

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
        let message = ProtocolCodec.encode(type: .stdin, payload: data)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func sendResize(cols: Int, rows: Int) {
        let payload = ResizePayload(cols: cols, rows: rows)
        let message = ProtocolCodec.encode(type: .resize, json: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }

    func disconnect() {
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil
        session?.invalidateAndCancel()
        session = nil
        continuation?.finish()
        continuation = nil
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
                continuation?.yield(.joined(info))
            }
        case .stdout:
            continuation?.yield(.stdout(payload))
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
