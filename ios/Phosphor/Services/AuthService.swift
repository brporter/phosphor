import AuthenticationServices
import Foundation

enum AuthService {

    enum AuthError: Error, LocalizedError {
        case invalidURL
        case loginFailed(Int)
        case invalidResponse
        case pollTimeout
        case cancelled

        var errorDescription: String? {
            switch self {
            case .invalidURL: return "Invalid relay URL"
            case .loginFailed(let code): return "Login failed: HTTP \(code)"
            case .invalidResponse: return "Invalid server response"
            case .pollTimeout: return "Authentication timed out"
            case .cancelled: return "Authentication cancelled"
            }
        }
    }

    struct LoginResponse: Decodable {
        let sessionId: String
        let authUrl: String

        enum CodingKeys: String, CodingKey {
            case sessionId = "session_id"
            case authUrl = "auth_url"
        }
    }

    struct PollResponse: Decodable {
        let status: String
        let idToken: String?

        enum CodingKeys: String, CodingKey {
            case status
            case idToken = "id_token"
        }
    }

    /// Initiate login by posting to /api/auth/login and opening the browser auth flow.
    @MainActor
    static func login(provider: String, baseURL: String, anchor: ASPresentationAnchor) async throws -> String {
        // Step 1: POST /api/auth/login with source "mobile" so the relay
        // redirects back to phosphor://auth/callback?session={id}
        guard let loginURL = URL(string: "\(baseURL)/api/auth/login") else {
            throw AuthError.invalidURL
        }

        var request = URLRequest(url: loginURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["provider": provider, "source": "mobile"])

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            let code = (response as? HTTPURLResponse)?.statusCode ?? 0
            throw AuthError.loginFailed(code)
        }

        let loginResp = try JSONDecoder().decode(LoginResponse.self, from: data)

        guard let authURL = URL(string: loginResp.authUrl) else {
            throw AuthError.invalidResponse
        }

        // Step 2: Open ASWebAuthenticationSession.
        // The relay redirects to phosphor://auth/callback?session={id} after
        // the OIDC exchange completes. ASWebAuthenticationSession detects the
        // custom scheme redirect and fires the callback immediately, dismissing
        // the browser.
        let anchorProvider = AnchorProvider(anchor: anchor)

        let callbackURL = try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<URL, Error>) in
            let authSession = ASWebAuthenticationSession(
                url: authURL,
                callbackURLScheme: "phosphor"
            ) { callbackURL, error in
                _ = anchorProvider  // prevent premature dealloc

                if let error = error {
                    if (error as NSError).code == ASWebAuthenticationSessionError.canceledLogin.rawValue {
                        continuation.resume(throwing: AuthError.cancelled)
                    } else {
                        continuation.resume(throwing: error)
                    }
                } else if let url = callbackURL {
                    continuation.resume(returning: url)
                } else {
                    continuation.resume(throwing: AuthError.invalidResponse)
                }
            }

            authSession.presentationContextProvider = anchorProvider
            authSession.prefersEphemeralWebBrowserSession = true
            authSession.start()
        }

        // Step 3: Extract session ID from callback URL and poll once for the token.
        // The relay has already completed the token exchange by the time it
        // redirected to our custom scheme, so the first poll should succeed.
        guard let components = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false),
              let sessionID = components.queryItems?.first(where: { $0.name == "session" })?.value else {
            throw AuthError.invalidResponse
        }

        return try await pollForToken(baseURL: baseURL, sessionId: sessionID)
    }

    /// Poll /api/auth/poll until the token is ready.
    static func pollForToken(baseURL: String, sessionId: String) async throws -> String {
        guard let pollURL = URL(string: "\(baseURL)/api/auth/poll?session=\(sessionId)") else {
            throw AuthError.invalidURL
        }

        // The token should be available immediately after the redirect, but
        // retry a few times in case of a race.
        for i in 0..<10 {
            if i > 0 {
                try await Task.sleep(for: .milliseconds(500))
            }

            let (data, _) = try await URLSession.shared.data(from: pollURL)
            let pollResp = try JSONDecoder().decode(PollResponse.self, from: data)

            if pollResp.status == "complete", let token = pollResp.idToken {
                return token
            }
        }

        throw AuthError.pollTimeout
    }
}

/// Provides the presentation anchor for ASWebAuthenticationSession.
private class AnchorProvider: NSObject, ASWebAuthenticationPresentationContextProviding {
    let anchor: ASPresentationAnchor

    init(anchor: ASPresentationAnchor) {
        self.anchor = anchor
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        anchor
    }
}
