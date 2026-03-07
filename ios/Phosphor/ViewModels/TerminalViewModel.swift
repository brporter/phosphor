import Foundation
import Observation

enum ConnectionState: String {
    case connecting
    case connected
    case disconnected
    case ended
    case error
}

@Observable
final class TerminalViewModel {
    var connectionState: ConnectionState = .disconnected
    var joinedInfo: JoinedPayload?
    var errorMessage: String?
    var viewerCount: Int = 0
    var processExitCode: Int?

    /// Callback invoked with stdout data to feed into SwiftTerm.
    var onStdout: ((Data) -> Void)?
    /// Callback invoked when the terminal should resize.
    var onResize: ((Int, Int) -> Void)?
    /// Callback invoked when the process exits (writes message to terminal).
    var onProcessExited: ((Int) -> Void)?

    private let wsManager = WebSocketManager()
    private var receiveTask: Task<Void, Never>?

    private var relayURL: String {
        UserDefaults.standard.string(forKey: "relay_url") ?? "https://phosphor.betaporter.dev"
    }

    var isPipeMode: Bool {
        joinedInfo?.mode == "pipe"
    }

    func connect(sessionId: String, token: String) {
        disconnect()
        connectionState = .connecting
        errorMessage = nil

        let stream = wsManager.connect(
            baseURL: relayURL,
            sessionId: sessionId,
            token: token
        )

        receiveTask = Task { [weak self] in
            for await event in stream {
                guard let self else { break }
                await self.handleEvent(event)
            }
        }
    }

    func disconnect() {
        receiveTask?.cancel()
        receiveTask = nil
        wsManager.disconnect()
        connectionState = .disconnected
    }

    func sendStdin(_ data: Data) {
        guard !isPipeMode else { return }
        wsManager.sendStdin(data)
    }

    func sendResize(cols: Int, rows: Int) {
        wsManager.sendResize(cols: cols, rows: rows)
    }

    func sendRestart() {
        wsManager.sendRestart()
    }

    @MainActor
    private func handleEvent(_ event: WebSocketEvent) {
        switch event {
        case .joined(let info):
            joinedInfo = info
            connectionState = .connected
            onResize?(info.cols, info.rows)

        case .stdout(let data):
            onStdout?(data)

        case .resize(let cols, let rows):
            onResize?(cols, rows)

        case .reconnect(let status):
            if status == "disconnected" {
                connectionState = .disconnected
            } else if status == "reconnected" {
                connectionState = .connected
            }

        case .viewerCount(let count):
            viewerCount = count

        case .mode(let mode):
            joinedInfo = joinedInfo.map {
                JoinedPayload(mode: mode, cols: $0.cols, rows: $0.rows, command: $0.command)
            }

        case .end:
            connectionState = .ended

        case .error(let message):
            errorMessage = message
            connectionState = .error

        case .processExited(let code):
            processExitCode = code
            onProcessExited?(code)

        case .restart:
            processExitCode = nil

        case .disconnected:
            if connectionState != .ended {
                connectionState = .disconnected
            }
        }
    }
}
