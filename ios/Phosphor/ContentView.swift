import SwiftUI

struct ContentView: View {
    let auth: AuthViewModel

    @State private var sessionListVM: SessionListViewModel?

    var body: some View {
        Group {
            if auth.isAuthenticated {
                authenticatedContent
            } else {
                LoginView(auth: auth)
            }
        }
        .onChange(of: auth.isAuthenticated) { _, isAuth in
            if isAuth {
                sessionListVM = SessionListViewModel(auth: auth)
            } else {
                sessionListVM?.stopPolling()
                sessionListVM = nil
            }
        }
    }

    @ViewBuilder
    private var authenticatedContent: some View {
        if let vm = sessionListVM {
            adaptiveNavigation(vm: vm)
        } else {
            ProgressView()
                .tint(PhosphorTheme.green)
                .onAppear {
                    sessionListVM = SessionListViewModel(auth: auth)
                }
        }
    }

    @ViewBuilder
    private func adaptiveNavigation(vm: SessionListViewModel) -> some View {
        #if os(macOS)
        NavigationSplitView {
            SessionListView(viewModel: vm, auth: auth)
                .navigationDestination(for: String.self) { sessionId in
                    TerminalContainerView(sessionId: sessionId, auth: auth)
                }
        } detail: {
            Text("Select a session")
                .font(.system(size: 16, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(PhosphorTheme.background)
        }
        #else
        NavigationStack {
            SessionListView(viewModel: vm, auth: auth)
                .navigationDestination(for: String.self) { sessionId in
                    TerminalContainerView(sessionId: sessionId, auth: auth)
                }
        }
        #endif
    }
}
