import Foundation
import CryptoKit
import CommonCrypto

struct CryptoManager: Sendable {

    static let iterations: UInt32 = 100_000
    static let keySize = 32  // AES-256

    // MARK: - Key Derivation

    /// Derive a 256-bit AES key from a passphrase and salt using PBKDF2-SHA256.
    /// Parameters must match Go (internal/crypto/crypto.go) and web (web/src/lib/crypto.ts).
    static func deriveKey(passphrase: String, salt: Data) -> SymmetricKey {
        let passphraseData = Data(passphrase.utf8)
        var derivedKeyBytes = [UInt8](repeating: 0, count: keySize)

        derivedKeyBytes.withUnsafeMutableBytes { derivedKeyBuffer in
            passphraseData.withUnsafeBytes { passphraseBuffer in
                salt.withUnsafeBytes { saltBuffer in
                    CCKeyDerivationPBKDF(
                        CCPBKDFAlgorithm(kCCPBKDF2),
                        passphraseBuffer.baseAddress?.assumingMemoryBound(to: Int8.self),
                        passphraseData.count,
                        saltBuffer.baseAddress?.assumingMemoryBound(to: UInt8.self),
                        salt.count,
                        CCPseudoRandomAlgorithm(kCCPRFHmacAlgSHA256),
                        iterations,
                        derivedKeyBuffer.baseAddress?.assumingMemoryBound(to: UInt8.self),
                        keySize
                    )
                }
            }
        }

        return SymmetricKey(data: derivedKeyBytes)
    }

    // MARK: - Errors

    enum CryptoError: Error {
        case encryptionFailed
        case ciphertextTooShort
    }

    // MARK: - Encrypt

    /// Encrypt plaintext using AES-256-GCM.
    /// Returns [12-byte nonce][ciphertext + 16-byte GCM tag].
    /// Wire format matches Go Encrypt() and web cryptoEncrypt().
    static func encrypt(key: SymmetricKey, plaintext: Data) throws -> Data {
        let sealedBox = try AES.GCM.seal(plaintext, using: key)
        // sealedBox.combined is nonce + ciphertext + tag
        guard let combined = sealedBox.combined else {
            throw CryptoError.encryptionFailed
        }
        return combined
    }

    // MARK: - Decrypt

    /// Decrypt data produced by Encrypt (Go/web/iOS).
    /// Expects [12-byte nonce][ciphertext + 16-byte GCM tag].
    /// Minimum input length: 28 bytes (12 nonce + 16 tag, zero-length plaintext).
    static func decrypt(key: SymmetricKey, data: Data) throws -> Data {
        guard data.count >= 28 else {
            throw CryptoError.ciphertextTooShort
        }
        let sealedBox = try AES.GCM.SealedBox(combined: data)
        return try AES.GCM.open(sealedBox, using: key)
    }

    // MARK: - Debug / Interop Verification

    #if DEBUG
    /// Verify cross-platform interop with Go crypto implementation.
    /// Salt: 16 zero bytes, passphrase: "test-passphrase".
    /// The derived key must match the Go output exactly.
    static func verifyCrossPlatformKeyDerivation() -> Bool {
        let salt = Data(repeating: 0, count: 16)
        let key = deriveKey(passphrase: "test-passphrase", salt: salt)
        let expectedBase64 = "r9YiIgH0Ka4xNmQmx/anXNNfAmqFPHaPFlQCeyz/tY="
        let expectedKey = Data(base64Encoded: expectedBase64)!
        return key.withUnsafeBytes { keyBytes in
            expectedKey.withUnsafeBytes { expectedBytes in
                keyBytes.elementsEqual(expectedBytes)
            }
        }
    }

    /// Round-trip test: encrypt then decrypt, verify plaintext matches.
    static func verifyRoundTrip() -> Bool {
        let salt = Data(repeating: 0, count: 16)
        let key = deriveKey(passphrase: "test", salt: salt)
        let plaintext = Data("hello, encrypted terminal!".utf8)
        guard let encrypted = try? encrypt(key: key, plaintext: plaintext),
              let decrypted = try? decrypt(key: key, data: encrypted) else {
            return false
        }
        return decrypted == plaintext
    }
    #endif
}
