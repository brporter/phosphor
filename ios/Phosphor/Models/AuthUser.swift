import Foundation

struct AuthUser: Codable {
    let idToken: String
    let sub: String
    let iss: String
    let email: String?

    enum CodingKeys: String, CodingKey {
        case idToken = "id_token"
        case sub, iss, email
    }

    /// Decode the JWT payload (middle segment) to extract claims.
    static func fromToken(_ token: String) -> AuthUser? {
        let parts = token.split(separator: ".")
        guard parts.count >= 2 else { return nil }

        var base64 = String(parts[1])
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")

        // Pad to multiple of 4
        let remainder = base64.count % 4
        if remainder > 0 {
            base64.append(contentsOf: String(repeating: "=", count: 4 - remainder))
        }

        guard let data = Data(base64Encoded: base64) else { return nil }

        struct JWTPayload: Decodable {
            let sub: String
            let iss: String
            let email: String?
            let exp: TimeInterval?
        }

        guard let payload = try? JSONDecoder().decode(JWTPayload.self, from: data) else {
            return nil
        }

        // Check expiry
        if let exp = payload.exp, Date(timeIntervalSince1970: exp) < Date() {
            return nil
        }

        return AuthUser(
            idToken: token,
            sub: payload.sub,
            iss: payload.iss,
            email: payload.email
        )
    }

    var isExpired: Bool {
        let parts = idToken.split(separator: ".")
        guard parts.count >= 2 else { return true }

        var base64 = String(parts[1])
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let remainder = base64.count % 4
        if remainder > 0 {
            base64.append(contentsOf: String(repeating: "=", count: 4 - remainder))
        }

        guard let data = Data(base64Encoded: base64),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let exp = json["exp"] as? TimeInterval else {
            return true
        }

        return Date(timeIntervalSince1970: exp) < Date()
    }
}
