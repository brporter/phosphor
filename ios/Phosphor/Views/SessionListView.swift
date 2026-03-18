import SwiftUI

struct SessionListView: View {
    let viewModel: SessionListViewModel
    let auth: AuthViewModel
    @State private var showSettings = false
    @State private var sessionToDestroy: SessionData?

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()
            ScanlineOverlay().ignoresSafeArea()

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
        .confirmationDialog(
            "Destroy Session",
            isPresented: Binding(
                get: { sessionToDestroy != nil },
                set: { if !$0 { sessionToDestroy = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button("Destroy Session", role: .destructive) {
                if let session = sessionToDestroy {
                    Task { await viewModel.destroySession(id: session.id) }
                }
                sessionToDestroy = nil
            }
            Button("Cancel", role: .cancel) {
                sessionToDestroy = nil
            }
        } message: {
            Text("This will permanently terminate the session and kill the remote process. You will need to re-run phosphor to start a new session.")
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
        List {
            ForEach(viewModel.sessions) { session in
                NavigationLink(value: session.id) {
                    SessionCardView(session: session)
                }
                .listRowBackground(PhosphorTheme.background)
                .listRowSeparator(.hidden)
                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                    Button(role: .destructive) {
                        sessionToDestroy = session
                    } label: {
                        Label("Destroy", systemImage: "trash")
                    }
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
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
