import Foundation
import Observation

@Observable
final class SessionListViewModel {
    var sessions: [SessionData] = []
    var isLoading = true
    var error: String?

    private var pollTask: Task<Void, Never>?
    private let auth: AuthViewModel

    init(auth: AuthViewModel) {
        self.auth = auth
    }

    private var relayURL: String {
        UserDefaults.standard.string(forKey: "relay_url") ?? "https://phosphor.betaporter.dev"
    }

    func startPolling() {
        stopPolling()
        pollTask = Task { [weak self] in
            guard let self else { return }
            while !Task.isCancelled {
                await self.refresh()
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    @MainActor
    func destroySession(id: String) async {
        guard let token = auth.getToken() else { return }

        do {
            try await APIClient.destroySession(
                baseURL: relayURL,
                id: id,
                token: token
            )
        } catch {
            // Session may already be gone
        }

        await refresh()
    }

    @MainActor
    func refresh() async {
        guard let token = auth.getToken() else {
            sessions = []
            error = "Not authenticated"
            isLoading = false
            return
        }

        do {
            let fetched = try await APIClient.fetchSessions(
                baseURL: relayURL,
                token: token
            )
            if sessions != fetched {
                sessions = fetched
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }

        isLoading = false
    }
}
