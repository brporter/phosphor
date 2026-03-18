import Foundation
import Observation
import CryptoKit

enum ConnectionState: String {
    case connecting
    case connected
    case disconnected
    case ended
    case error
}

struct FileTransfer: Identifiable {
    let id: String
    let name: String
    let size: Int
    var bytesWritten: Int = 0
    var status: FileTransferStatus = .uploading
    var error: String?
}

enum FileTransferStatus {
    case uploading
    case complete
    case error
}

private let fileChunkSize = 32 * 1024  // 32KB
private let transferIDLength = 8
private let ackTimeoutSeconds: UInt64 = 30
private let completedTransferTTLSeconds: UInt64 = 10

@Observable
final class TerminalViewModel {
    var connectionState: ConnectionState = .disconnected
    var joinedInfo: JoinedPayload?
    var errorMessage: String?
    var viewerCount: Int = 0
    var processExitCode: Int?
    var fileTransfers: [String: FileTransfer] = [:]

    /// Callback invoked with stdout data to feed into SwiftTerm.
    var onStdout: ((Data) -> Void)?
    /// Callback invoked when the terminal should resize.
    var onResize: ((Int, Int) -> Void)?
    /// Callback invoked when the process exits (writes message to terminal).
    var onProcessExited: ((Int) -> Void)?

    private let wsManager = WebSocketManager()
    private var receiveTask: Task<Void, Never>?
    private var pendingAcks: [String: CheckedContinuation<FileAckPayload, Never>] = [:]

    private var relayURL: String {
        UserDefaults.standard.string(forKey: "relay_url") ?? "https://phosphor.betaporter.dev"
    }

    var isPipeMode: Bool {
        joinedInfo?.mode == "pipe"
    }

    var activeUploads: [FileTransfer] {
        fileTransfers.values.filter { $0.status == .uploading || $0.status == .error }
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

    func sendFile(url: URL) {
        guard !isPipeMode else { return }

        Task {
            do {
                let data = try Data(contentsOf: url)
                let id = Self.generateTransferID()
                let name = url.lastPathComponent
                let size = data.count

                await MainActor.run {
                    fileTransfers[id] = FileTransfer(id: id, name: name, size: size)
                }

                // Send FileStart and wait for acceptance ack
                wsManager.sendFileStart(id: id, name: name, size: size)

                let startAck = await waitForAck(id: id)
                if startAck.status == "error" {
                    await markError(id: id, message: startAck.error ?? "upload rejected")
                    return
                }

                // Stream file in chunks
                var offset = 0
                while offset < size {
                    let end = min(offset + fileChunkSize, size)
                    let chunk = data[offset..<end]
                    wsManager.sendFileChunk(id: id, chunk: Data(chunk))
                    offset = end
                }

                // Compute SHA256
                let digest = SHA256.hash(data: data)
                let sha256 = digest.map { String(format: "%02x", $0) }.joined()

                // Send FileEnd
                wsManager.sendFileEnd(id: id, sha256: sha256)

                // Wait for completion ack
                let completeAck = await waitForAck(id: id)
                if completeAck.status == "error" {
                    await markError(id: id, message: completeAck.error ?? "upload failed")
                    return
                }

                // Schedule cleanup
                scheduleTransferCleanup(id: id)
            } catch {
                // File read error — no transfer to track
            }
        }
    }

    private func waitForAck(id: String) async -> FileAckPayload {
        await withCheckedContinuation { continuation in
            pendingAcks[id] = continuation

            // Timeout
            Task {
                try? await Task.sleep(nanoseconds: ackTimeoutSeconds * 1_000_000_000)
                if let pending = pendingAcks.removeValue(forKey: id) {
                    pending.resume(returning: FileAckPayload(id: id, status: "error", error: "ack timeout", bytesWritten: nil))
                }
            }
        }
    }

    @MainActor
    private func markError(id: String, message: String) {
        if var transfer = fileTransfers[id] {
            transfer.status = .error
            transfer.error = message
            fileTransfers[id] = transfer
        }
        scheduleTransferCleanup(id: id)
    }

    private func scheduleTransferCleanup(id: String) {
        Task {
            try? await Task.sleep(nanoseconds: completedTransferTTLSeconds * 1_000_000_000)
            await MainActor.run {
                if let transfer = fileTransfers[id], transfer.status != .uploading {
                    fileTransfers.removeValue(forKey: id)
                }
            }
        }
    }

    private static func generateTransferID() -> String {
        let chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
        var result = ""
        for _ in 0..<transferIDLength {
            result.append(chars.randomElement()!)
        }
        return result
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

        case .fileAck(let ack):
            // Resolve pending ack continuation
            if let pending = pendingAcks.removeValue(forKey: ack.id) {
                pending.resume(returning: ack)
            }
            // Update transfer state
            if var transfer = fileTransfers[ack.id] {
                switch ack.status {
                case "accepted":
                    transfer.status = .uploading
                case "progress":
                    transfer.bytesWritten = ack.bytesWritten ?? transfer.bytesWritten
                case "complete":
                    transfer.status = .complete
                    transfer.bytesWritten = ack.bytesWritten ?? transfer.size
                    scheduleTransferCleanup(id: ack.id)
                case "error":
                    transfer.status = .error
                    transfer.error = ack.error
                    scheduleTransferCleanup(id: ack.id)
                default:
                    break
                }
                fileTransfers[ack.id] = transfer
            }

        case .disconnected:
            if connectionState != .ended {
                connectionState = .disconnected
            }
            // Reject pending ack continuations
            for (id, pending) in pendingAcks {
                pending.resume(returning: FileAckPayload(id: id, status: "error", error: "connection closed", bytesWritten: nil))
            }
            pendingAcks.removeAll()
        }
    }
}
