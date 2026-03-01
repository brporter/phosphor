import SwiftUI

struct SessionListView: View {
    let viewModel: SessionListViewModel
    let auth: AuthViewModel

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()

            if viewModel.isLoading && viewModel.sessions.isEmpty {
                ProgressView()
                    .tint(PhosphorTheme.green)
            } else if viewModel.sessions.isEmpty {
                emptyState
            } else {
                sessionList
            }
        }
        .navigationTitle("Active Sessions")
        #if !os(macOS)
        .toolbarColorScheme(.dark, for: .navigationBar)
        #endif
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink(destination: SettingsView(auth: auth)) {
                    Image(systemName: "gear")
                        .foregroundStyle(PhosphorTheme.text)
                }
            }
        }
        .refreshable {
            await viewModel.refresh()
        }
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
    }

    private var sessionList: some View {
        ScrollView {
            LazyVStack(spacing: 8) {
                ForEach(viewModel.sessions) { session in
                    NavigationLink(value: session.id) {
                        SessionCardView(session: session)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal)
            .padding(.top, 8)
        }
    }

    private var emptyState: some View {
        VStack(spacing: 16) {
            Image(systemName: "terminal")
                .font(.system(size: 48))
                .foregroundStyle(PhosphorTheme.text.opacity(0.3))

            Text("No active sessions")
                .font(.system(size: 18, weight: .medium, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text)

            Text("Start a terminal session with:\nphosphor -- bash")
                .font(.system(size: 14, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text.opacity(0.6))
                .multilineTextAlignment(.center)

            if let error = viewModel.error {
                Text(error)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.red)
            }
        }
    }
}
