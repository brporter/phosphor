import SwiftUI
import UniformTypeIdentifiers

struct TerminalContainerView: View {
    let sessionId: String
    let auth: AuthViewModel
    @State private var viewModel = TerminalViewModel()
    @State private var showFilePicker = false

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()
            ScanlineOverlay().ignoresSafeArea()

            VStack(spacing: 0) {
                // Status bar
                statusBar

                // Upload progress
                if !viewModel.activeUploads.isEmpty {
                    uploadProgressBar
                }

                // Terminal
                switch viewModel.connectionState {
                case .connecting:
                    Spacer()
                    VStack(spacing: 12) {
                        ProgressView()
                            .tint(PhosphorTheme.green)
                        Text("Connecting...")
                            .font(.system(size: 14, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.text)
                    }
                    Spacer()

                case .error:
                    Spacer()
                    VStack(spacing: 12) {
                        Image(systemName: "exclamationmark.triangle")
                            .font(.system(size: 32))
                            .foregroundStyle(PhosphorTheme.red)
                        Text(viewModel.errorMessage ?? "Connection error")
                            .font(.system(size: 14, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.red)
                            .multilineTextAlignment(.center)
                            .padding(.horizontal)
                    }
                    Spacer()

                case .ended:
                    Spacer()
                    VStack(spacing: 12) {
                        Image(systemName: "terminal")
                            .font(.system(size: 32))
                            .foregroundStyle(PhosphorTheme.text.opacity(0.5))
                        Text("Session ended")
                            .font(.system(size: 14, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.text)
                    }
                    Spacer()

                default:
                    TerminalRepresentable(viewModel: viewModel)
                        .ignoresSafeArea(.keyboard, edges: .bottom)
                }
            }
        }
        #if !os(macOS)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarColorScheme(.dark, for: .navigationBar)
        #endif
        .fileImporter(
            isPresented: $showFilePicker,
            allowedContentTypes: [.item],
            allowsMultipleSelection: false
        ) { result in
            if case .success(let urls) = result, let url = urls.first {
                if url.startAccessingSecurityScopedResource() {
                    viewModel.sendFile(url: url)
                    url.stopAccessingSecurityScopedResource()
                }
            }
        }
        .onAppear {
            if let token = auth.getToken() {
                viewModel.connect(sessionId: sessionId, token: token)
                viewModel.onProcessExited = { code in
                    let message = "\r\n\u{1B}[1;33m[Process exited (code \(code))]\u{1B}[0m\r\n"
                    if let data = message.data(using: .utf8) {
                        viewModel.onStdout?(data)
                    }
                }
            }
        }
        .onDisappear {
            viewModel.disconnect()
        }
    }

    private var statusBar: some View {
        HStack(spacing: 12) {
            // Connection status dot
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)

            if let info = viewModel.joinedInfo {
                Text(info.command)
                    .font(.system(size: 12, weight: .medium, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.green)
                    .glowText()
                    .lineLimit(1)

                Text(info.mode.uppercased())
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.background)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(PhosphorTheme.amber)
                    .clipShape(RoundedRectangle(cornerRadius: 3))

                if viewModel.isPipeMode {
                    Text("VIEW ONLY")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.cyan)
                }
            }

            Spacer()

            // Upload button (PTY mode only)
            if viewModel.connectionState == .connected && !viewModel.isPipeMode {
                Button {
                    showFilePicker = true
                } label: {
                    Label("Upload", systemImage: "arrow.up.doc")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.green)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .overlay(
                            RoundedRectangle(cornerRadius: 4)
                                .strokeBorder(PhosphorTheme.green, lineWidth: 1)
                        )
                }
            }

            if viewModel.viewerCount > 0 {
                Label("\(viewModel.viewerCount)", systemImage: "eye")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text)
            }

            if let exitCode = viewModel.processExitCode {
                HStack(spacing: 8) {
                    Text("exited (\(exitCode))")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.amber)

                    Button {
                        viewModel.sendRestart()
                    } label: {
                        Text("restart")
                            .font(.system(size: 10, weight: .semibold, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.green)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .overlay(
                                RoundedRectangle(cornerRadius: 4)
                                    .strokeBorder(PhosphorTheme.green, lineWidth: 1)
                            )
                    }
                }
            } else {
                Text(viewModel.connectionState.rawValue)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text.opacity(0.6))
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(PhosphorTheme.panel)
    }

    private var uploadProgressBar: some View {
        VStack(spacing: 4) {
            ForEach(viewModel.activeUploads) { transfer in
                HStack(spacing: 8) {
                    Text(transfer.name)
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.text)
                        .lineLimit(1)

                    if transfer.status == .error {
                        Text(transfer.error ?? "error")
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.red)
                            .lineLimit(1)
                    } else {
                        GeometryReader { geo in
                            let pct = transfer.size > 0
                                ? CGFloat(transfer.bytesWritten) / CGFloat(transfer.size)
                                : 0
                            ZStack(alignment: .leading) {
                                RoundedRectangle(cornerRadius: 2)
                                    .fill(PhosphorTheme.border)
                                    .frame(height: 6)
                                RoundedRectangle(cornerRadius: 2)
                                    .fill(PhosphorTheme.green)
                                    .frame(width: geo.size.width * pct, height: 6)
                            }
                        }
                        .frame(height: 6)

                        let pctInt = transfer.size > 0
                            ? Int(Double(transfer.bytesWritten) / Double(transfer.size) * 100)
                            : 0
                        Text("\(pctInt)%")
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(PhosphorTheme.green)
                            .frame(width: 36, alignment: .trailing)
                    }
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 4)
        .background(PhosphorTheme.panel)
    }

    private var statusColor: Color {
        switch viewModel.connectionState {
        case .connected:
            return viewModel.processExitCode != nil ? PhosphorTheme.amber : PhosphorTheme.green
        case .connecting: return PhosphorTheme.amber
        case .disconnected: return PhosphorTheme.amber
        case .ended: return PhosphorTheme.text
        case .error: return PhosphorTheme.red
        }
    }
}
