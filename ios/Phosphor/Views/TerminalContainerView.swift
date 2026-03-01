import SwiftUI

struct TerminalContainerView: View {
    let sessionId: String
    let auth: AuthViewModel
    @State private var viewModel = TerminalViewModel()

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()

            VStack(spacing: 0) {
                // Status bar
                statusBar

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
        .onAppear {
            if let token = auth.getToken() {
                viewModel.connect(sessionId: sessionId, token: token)
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

            if viewModel.viewerCount > 0 {
                Label("\(viewModel.viewerCount)", systemImage: "eye")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text)
            }

            Text(viewModel.connectionState.rawValue)
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text.opacity(0.6))
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(PhosphorTheme.panel)
    }

    private var statusColor: Color {
        switch viewModel.connectionState {
        case .connected: return PhosphorTheme.green
        case .connecting: return PhosphorTheme.amber
        case .disconnected: return PhosphorTheme.amber
        case .ended: return PhosphorTheme.text
        case .error: return PhosphorTheme.red
        }
    }
}
