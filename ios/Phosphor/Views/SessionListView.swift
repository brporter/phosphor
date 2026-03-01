import SwiftUI

struct SessionListView: View {
    let viewModel: SessionListViewModel
    let auth: AuthViewModel
    @State private var showSettings = false

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
                Button {
                    showSettings = true
                } label: {
                    Image(systemName: "gear")
                        .foregroundStyle(PhosphorTheme.text)
                }
            }
        }
        .sheet(isPresented: $showSettings) {
            NavigationStack {
                SettingsView(auth: auth)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") { showSettings = false }
                        }
                    }
            }
            .preferredColorScheme(.dark)
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
