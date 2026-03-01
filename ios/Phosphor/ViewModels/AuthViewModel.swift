import AuthenticationServices
import Foundation
import Observation

@Observable
final class AuthViewModel {
    var user: AuthUser?
    var isLoading = false
    var error: String?

    var isAuthenticated: Bool { user != nil }

    private var relayURL: String {
        UserDefaults.standard.string(forKey: "relay_url") ?? "https://phosphor.betaporter.dev"
    }

    func loadCachedUser() {
        guard let token = KeychainHelper.load(forKey: KeychainHelper.idTokenKey) else {
            return
        }

        guard let cachedUser = AuthUser.fromToken(token), !cachedUser.isExpired else {
            KeychainHelper.delete(forKey: KeychainHelper.idTokenKey)
            return
        }

        user = cachedUser
    }

    @MainActor
    func login(provider: String, anchor: ASPresentationAnchor) async {
        isLoading = true
        error = nil

        do {
            let token = try await AuthService.login(
                provider: provider,
                baseURL: relayURL,
                anchor: anchor
            )

            guard let newUser = AuthUser.fromToken(token) else {
                error = "Invalid token received"
                isLoading = false
                return
            }

            KeychainHelper.save(token: token, forKey: KeychainHelper.idTokenKey)
            user = newUser
        } catch is CancellationError {
            // User cancelled — not an error
        } catch let authError as AuthService.AuthError where authError == .cancelled {
            // User cancelled — not an error
        } catch {
            self.error = error.localizedDescription
        }

        isLoading = false
    }

    func logout() {
        KeychainHelper.delete(forKey: KeychainHelper.idTokenKey)
        user = nil
    }

    func getToken() -> String? {
        user?.idToken
    }
}

// Allow equatable comparison for AuthService.AuthError
extension AuthService.AuthError: Equatable {
    static func == (lhs: AuthService.AuthError, rhs: AuthService.AuthError) -> Bool {
        switch (lhs, rhs) {
        case (.invalidURL, .invalidURL): return true
        case (.loginFailed(let a), .loginFailed(let b)): return a == b
        case (.invalidResponse, .invalidResponse): return true
        case (.pollTimeout, .pollTimeout): return true
        case (.cancelled, .cancelled): return true
        default: return false
        }
    }
}
