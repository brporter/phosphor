import Foundation

enum APIClient {

    enum APIError: Error, LocalizedError {
        case invalidURL
        case httpError(Int)
        case decodingError

        var errorDescription: String? {
            switch self {
            case .invalidURL: return "Invalid URL"
            case .httpError(let code): return "HTTP error: \(code)"
            case .decodingError: return "Failed to decode response"
            }
        }
    }

    static func fetchSessions(baseURL: String, token: String) async throws -> [SessionData] {
        guard let url = URL(string: "\(baseURL)/api/sessions") else {
            throw APIError.invalidURL
        }

        var request = URLRequest(url: url)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.httpError(0)
        }

        guard httpResponse.statusCode == 200 else {
            throw APIError.httpError(httpResponse.statusCode)
        }

        return try JSONDecoder().decode([SessionData].self, from: data)
    }
}
