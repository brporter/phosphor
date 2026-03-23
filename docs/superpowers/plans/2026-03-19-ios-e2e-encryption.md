# iOS E2E Encryption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end encryption support to the Phosphor iOS viewer app so it can decrypt encrypted terminal sessions and encrypt stdin, matching the existing Go CLI and web client implementations.

**Architecture:** A new `CryptoManager` service provides PBKDF2-SHA256 key derivation and AES-256-GCM encrypt/decrypt. The `WebSocketManager` gains encryption awareness (buffering, decrypt/encrypt). The `TerminalViewModel` manages encryption state and passphrase submission. A new `PassphraseView` prompts the user.

**Tech Stack:** Swift, CryptoKit (AES-GCM), CommonCrypto (PBKDF2), SwiftUI

**Spec:** `docs/superpowers/specs/2026-03-19-ios-e2e-encryption-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `ios/Phosphor/Services/CryptoManager.swift` | Create | PBKDF2 key derivation + AES-256-GCM encrypt/decrypt |
| `ios/Phosphor/Protocol/ProtocolCodec.swift` | Modify | Add `encrypted`, `encryptionSalt` to `JoinedPayload` |
| `ios/Phosphor/Services/WebSocketManager.swift` | Modify | New events, stdout buffering, encrypt/decrypt data path |
| `ios/Phosphor/ViewModels/TerminalViewModel.swift` | Modify | `EncryptionState`, passphrase submission, key lifecycle |
| `ios/Phosphor/Views/PassphraseView.swift` | Create | Themed passphrase entry UI |
| `ios/Phosphor/Views/TerminalContainerView.swift` | Modify | Show `PassphraseView` when encryption requires passphrase |

---

### Task 1: CryptoManager — Key Derivation

**Files:**
- Create: `ios/Phosphor/Services/CryptoManager.swift`

This task builds the PBKDF2 key derivation function. AES-GCM encrypt/decrypt are added in Tasks 2-3.

- [ ] **Step 1: Create CryptoManager with deriveKey**

Create `ios/Phosphor/Services/CryptoManager.swift`:

```swift
import Foundation
import CryptoKit
import CommonCrypto

struct CryptoManager: Sendable {

    static let iterations: UInt32 = 100_000
    static let keySize = 32  // AES-256

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
}
```

- [ ] **Step 2: Verify it compiles**

Build the iOS project in Xcode or via:
```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 3: Commit**

```bash
git add ios/Phosphor/Services/CryptoManager.swift
git commit -m "feat(ios): add CryptoManager with PBKDF2-SHA256 key derivation"
```

---

### Task 2: CryptoManager — Encrypt

**Files:**
- Modify: `ios/Phosphor/Services/CryptoManager.swift`

- [ ] **Step 1: Add encrypt function**

Add to `CryptoManager` struct, after `deriveKey`:

```swift
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
```

Also add the error enum at the top of the struct:

```swift
    enum CryptoError: Error {
        case encryptionFailed
        case ciphertextTooShort
    }
```

- [ ] **Step 2: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 3: Commit**

```bash
git add ios/Phosphor/Services/CryptoManager.swift
git commit -m "feat(ios): add AES-256-GCM encrypt to CryptoManager"
```

---

### Task 3: CryptoManager — Decrypt

**Files:**
- Modify: `ios/Phosphor/Services/CryptoManager.swift`

- [ ] **Step 1: Add decrypt function**

Add to `CryptoManager` struct, after `encrypt`:

```swift
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
```

- [ ] **Step 2: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 3: Commit**

```bash
git add ios/Phosphor/Services/CryptoManager.swift
git commit -m "feat(ios): add AES-256-GCM decrypt to CryptoManager"
```

---

### Task 4: CryptoManager — Cross-Platform Interop Test

**Files:**
- Modify: `ios/Phosphor/Services/CryptoManager.swift` (add test vector constants, or use a test target)

Since the iOS project may not have a test target set up, we'll generate a known test vector from Go and add a static verification method that can be called at debug time. Alternatively, if a test target exists, write a proper XCTest.

- [ ] **Step 1: Generate a test vector from Go**

Create a small Go program to print a known test vector:

```bash
cd /Users/brporter/projects/phosphor
cat > /tmp/gen_test_vector.go << 'GOEOF'
package main

import (
    "encoding/base64"
    "fmt"
    phcrypto "phosphor/internal/crypto"
)

func main() {
    salt, _ := base64.StdEncoding.DecodeString("AAAAAAAAAAAAAAAAAAAAAA==")
    key, _ := phcrypto.DeriveKey("test-passphrase", salt)
    fmt.Printf("Key (base64): %s\n", base64.StdEncoding.EncodeToString(key))

    plaintext := []byte("hello, encrypted terminal!")
    encrypted, _ := phcrypto.Encrypt(key, plaintext)
    fmt.Printf("Encrypted (base64): %s\n", base64.StdEncoding.EncodeToString(encrypted))

    decrypted, _ := phcrypto.Decrypt(key, encrypted)
    fmt.Printf("Decrypted: %s\n", string(decrypted))
}
GOEOF
go run /tmp/gen_test_vector.go
```

Record the key derivation output. The encrypted output varies (random nonce), but the key for passphrase `"test-passphrase"` + salt `[16 zero bytes]` is deterministic.

- [ ] **Step 2: Add a static self-test method to CryptoManager**

```swift
    #if DEBUG
    /// Verify cross-platform interop with Go crypto implementation.
    /// Salt: 16 zero bytes, passphrase: "test-passphrase".
    /// The derived key must match the Go output exactly.
    static func verifyCrossPlatformKeyDerivation() -> Bool {
        let salt = Data(repeating: 0, count: 16)
        let key = deriveKey(passphrase: "test-passphrase", salt: salt)
        // Replace with actual base64 from Step 1
        let expectedBase64 = "REPLACE_WITH_GO_OUTPUT"
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
```

Update `expectedBase64` with the actual value from Step 1.

- [ ] **Step 3: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```

- [ ] **Step 4: Commit**

```bash
git add ios/Phosphor/Services/CryptoManager.swift
git commit -m "test(ios): add cross-platform key derivation verification to CryptoManager"
```

---

### Task 5: Protocol — Add Encryption Fields to JoinedPayload

**Files:**
- Modify: `ios/Phosphor/Protocol/ProtocolCodec.swift:73-78`

- [ ] **Step 1: Add encrypted and encryptionSalt fields**

Replace the `JoinedPayload` struct (lines 73-78) with:

```swift
struct JoinedPayload: Codable {
    let mode: String
    let cols: Int
    let rows: Int
    let command: String
    let encrypted: Bool?
    let encryptionSalt: String?

    enum CodingKeys: String, CodingKey {
        case mode, cols, rows, command, encrypted
        case encryptionSalt = "encryption_salt"
    }
}
```

- [ ] **Step 2: Verify it compiles**

The `encrypted` and `encryptionSalt` fields are optional, so existing code that constructs `JoinedPayload` without them will still compile. Check:

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```

Expected: BUILD SUCCEEDED. If there are compilation errors in `TerminalViewModel.swift:227-229` where `JoinedPayload` is reconstructed in the `.mode` handler, fix it in this step — see Step 3.

- [ ] **Step 3: Fix the .mode handler in TerminalViewModel**

In `ios/Phosphor/ViewModels/TerminalViewModel.swift:226-229`, the `.mode` event reconstructs `JoinedPayload`. Update it to carry forward the new fields:

Replace:
```swift
        case .mode(let mode):
            joinedInfo = joinedInfo.map {
                JoinedPayload(mode: mode, cols: $0.cols, rows: $0.rows, command: $0.command)
            }
```

With:
```swift
        case .mode(let mode):
            joinedInfo = joinedInfo.map {
                JoinedPayload(mode: mode, cols: $0.cols, rows: $0.rows, command: $0.command, encrypted: $0.encrypted, encryptionSalt: $0.encryptionSalt)
            }
```

- [ ] **Step 4: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 5: Commit**

```bash
git add ios/Phosphor/Protocol/ProtocolCodec.swift ios/Phosphor/ViewModels/TerminalViewModel.swift
git commit -m "feat(ios): add encryption fields to JoinedPayload protocol"
```

---

### Task 6: WebSocketManager — Encryption Events and Buffering

**Files:**
- Modify: `ios/Phosphor/Services/WebSocketManager.swift`

- [ ] **Step 1: Add new event cases**

In the `WebSocketEvent` enum (lines 4-17), add two new cases:

```swift
    case encryptionRequired(salt: String)
    case decryptionFailed(rawData: Data)
```

- [ ] **Step 2: Add import and encryption state properties**

Add `import CryptoKit` at the top of `WebSocketManager.swift` (after `import Foundation`).

Add these properties to the `WebSocketManager` class (after line 24):

```swift
    private var encryptionKey: SymmetricKey?
    private var isEncrypted = false
    private var bufferedEncryptedStdout: [Data] = []
```

- [ ] **Step 3: Update handleBinaryMessage for Joined**

In `handleBinaryMessage` (line 149-152), replace the `.joined` case:

```swift
        case .joined:
            if let info: JoinedPayload = try? ProtocolCodec.decodeJSON(payload) {
                if info.encrypted == true, let salt = info.encryptionSalt {
                    isEncrypted = true
                    continuation?.yield(.encryptionRequired(salt: salt))
                }
                continuation?.yield(.joined(info))
            }
```

- [ ] **Step 4: Update handleBinaryMessage for Stdout**

Replace the `.stdout` case (line 153-154):

```swift
        case .stdout:
            if isEncrypted {
                if let key = encryptionKey {
                    do {
                        let decrypted = try CryptoManager.decrypt(key: key, data: payload)
                        continuation?.yield(.stdout(decrypted))
                    } catch {
                        continuation?.yield(.decryptionFailed(rawData: payload))
                    }
                } else {
                    bufferedEncryptedStdout.append(payload)
                }
            } else {
                continuation?.yield(.stdout(payload))
            }
```

- [ ] **Step 5: Update sendStdin for encryption**

Replace `sendStdin` method (lines 73-76):

```swift
    func sendStdin(_ data: Data) {
        let payload: Data
        if isEncrypted, let key = encryptionKey {
            guard let encrypted = try? CryptoManager.encrypt(key: key, plaintext: data) else { return }
            payload = encrypted
        } else {
            payload = data
        }
        let message = ProtocolCodec.encode(type: .stdin, payload: payload)
        webSocketTask?.send(.data(message)) { _ in }
    }
```

- [ ] **Step 6: Add setEncryptionKey and clearEncryptionKey methods**

Add after `sendRestart()` (line 87):

```swift
    /// Set the derived encryption key and flush buffered stdout.
    /// Returns false if any buffered chunk fails to decrypt (wrong key).
    func setEncryptionKey(_ key: SymmetricKey) -> Bool {
        // Trial-decrypt first buffered chunk to validate key
        if let firstChunk = bufferedEncryptedStdout.first {
            do {
                _ = try CryptoManager.decrypt(key: key, data: firstChunk)
            } catch {
                return false
            }
        }

        self.encryptionKey = key

        // Flush buffered stdout
        for chunk in bufferedEncryptedStdout {
            if let decrypted = try? CryptoManager.decrypt(key: key, data: chunk) {
                continuation?.yield(.stdout(decrypted))
            }
        }
        bufferedEncryptedStdout.removeAll()
        return true
    }

    func clearEncryptionKey() {
        encryptionKey = nil
    }

    func reBufferChunk(_ data: Data) {
        bufferedEncryptedStdout.append(data)
    }
```

- [ ] **Step 7: Reset encryption state on disconnect**

In the `disconnect()` method (lines 111-118), add cleanup:

```swift
    func disconnect() {
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil
        session?.invalidateAndCancel()
        session = nil
        continuation?.finish()
        continuation = nil
        encryptionKey = nil
        isEncrypted = false
        bufferedEncryptedStdout.removeAll()
    }
```

- [ ] **Step 8: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED (there may be warnings about unhandled event cases in TerminalViewModel — those are fixed in Task 7)

- [ ] **Step 9: Commit**

```bash
git add ios/Phosphor/Services/WebSocketManager.swift
git commit -m "feat(ios): add encryption support to WebSocketManager with buffering"
```

---

### Task 7: TerminalViewModel — Encryption State and Passphrase Handling

**Files:**
- Modify: `ios/Phosphor/ViewModels/TerminalViewModel.swift`

- [ ] **Step 1: Add EncryptionState enum and properties**

Add after the `ConnectionState` enum (after line 11):

```swift
enum EncryptionState: Equatable {
    case none
    case passphraseRequired(salt: String)
    case unlocked
    case failed(salt: String)
}
```

Add to `TerminalViewModel` class properties (after `processExitCode` on line 39):

```swift
    var encryptionState: EncryptionState = .none
```

- [ ] **Step 2: Add submitPassphrase method**

Add after `sendRestart()` (line 100-102):

```swift
    func submitPassphrase(_ passphrase: String, salt: String) {
        guard let saltData = Data(base64Encoded: salt) else {
            encryptionState = .failed(salt: salt)
            return
        }
        let key = CryptoManager.deriveKey(passphrase: passphrase, salt: saltData)

        if wsManager.setEncryptionKey(key) {
            encryptionState = .unlocked
        } else {
            encryptionState = .failed(salt: salt)
        }
    }
```

- [ ] **Step 3: Handle new WebSocket events in handleEvent**

In `handleEvent` (line 203), update the `.joined` case (lines 205-208):

```swift
        case .joined(let info):
            joinedInfo = info
            if info.encrypted == true {
                // Connection state stays .connecting until passphrase is submitted
                // encryptionRequired event will set the encryption state
            } else {
                connectionState = .connected
            }
            onResize?(info.cols, info.rows)
```

Add new cases before the `.disconnected` case:

```swift
        case .encryptionRequired(let salt):
            encryptionState = .passphraseRequired(salt: salt)

        case .decryptionFailed(let rawData):
            wsManager.clearEncryptionKey()
            wsManager.reBufferChunk(rawData)
            if case .unlocked = encryptionState,
               let salt = joinedInfo?.encryptionSalt {
                encryptionState = .failed(salt: salt)
            }
```

- [ ] **Step 4: Update submitPassphrase to transition connectionState**

Update `submitPassphrase` — when key is accepted, also set `connectionState`:

```swift
    func submitPassphrase(_ passphrase: String, salt: String) {
        guard let saltData = Data(base64Encoded: salt) else {
            encryptionState = .failed(salt: salt)
            return
        }
        let key = CryptoManager.deriveKey(passphrase: passphrase, salt: saltData)

        if wsManager.setEncryptionKey(key) {
            encryptionState = .unlocked
            connectionState = .connected
        } else {
            encryptionState = .failed(salt: salt)
        }
    }
```

(This replaces the version from Step 2.)

- [ ] **Step 5: Reset encryption state on disconnect**

In `TerminalViewModel.disconnect()` (line 84-89), add `encryptionState = .none`:

```swift
    func disconnect() {
        receiveTask?.cancel()
        receiveTask = nil
        wsManager.disconnect()
        connectionState = .disconnected
        encryptionState = .none
    }
```

- [ ] **Step 6: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 7: Commit**

```bash
git add ios/Phosphor/ViewModels/TerminalViewModel.swift
git commit -m "feat(ios): add encryption state management to TerminalViewModel"
```

---

### Task 8: PassphraseView

**Files:**
- Create: `ios/Phosphor/Views/PassphraseView.swift`

- [ ] **Step 1: Create PassphraseView**

Create `ios/Phosphor/Views/PassphraseView.swift`:

```swift
import SwiftUI

struct PassphraseView: View {
    let salt: String
    let isFailed: Bool
    let onSubmit: (String) -> Void

    @State private var passphrase = ""
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(spacing: 24) {
            Spacer()

            Image(systemName: "lock.fill")
                .font(.system(size: 48))
                .foregroundStyle(PhosphorTheme.green)
                .glowText(radius: 12, opacity: 0.5)

            Text("Encrypted Session")
                .font(.system(size: 20, weight: .bold, design: .monospaced))
                .foregroundStyle(PhosphorTheme.green)
                .glowText()

            Text("Enter the passphrase to decrypt this session")
                .font(.system(size: 13, design: .monospaced))
                .foregroundStyle(PhosphorTheme.text)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 32)

            if isFailed {
                Text("Incorrect passphrase. Try again.")
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.red)
            }

            VStack(spacing: 16) {
                SecureField("Passphrase", text: $passphrase)
                    .font(.system(size: 14, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.textBright)
                    .padding(12)
                    .background(PhosphorTheme.panel)
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .strokeBorder(
                                isFailed ? PhosphorTheme.red : PhosphorTheme.green,
                                lineWidth: 1
                            )
                    )
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                    .focused($isFocused)
                    .onSubmit { submit() }

                Button(action: submit) {
                    Text("Unlock")
                        .font(.system(size: 14, weight: .semibold, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.background)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 12)
                        .background(PhosphorTheme.green)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                }
                .disabled(passphrase.isEmpty)
                .opacity(passphrase.isEmpty ? 0.5 : 1.0)
            }
            .padding(.horizontal, 32)

            Spacer()
            Spacer()
        }
        .onAppear {
            isFocused = true
        }
    }

    private func submit() {
        guard !passphrase.isEmpty else { return }
        let submitted = passphrase
        passphrase = ""
        onSubmit(submitted)
    }
}
```

- [ ] **Step 2: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 3: Commit**

```bash
git add ios/Phosphor/Views/PassphraseView.swift
git commit -m "feat(ios): add PassphraseView for encrypted session entry"
```

---

### Task 9: TerminalContainerView — Integration

**Files:**
- Modify: `ios/Phosphor/Views/TerminalContainerView.swift`

- [ ] **Step 1: Add encryption state switching**

In `TerminalContainerView`, replace the terminal display section. The current `switch viewModel.connectionState` block (lines 25-66) needs an encryption check added. Replace the `default:` case (lines 63-66) which shows the terminal:

```swift
                default:
                    switch viewModel.encryptionState {
                    case .passphraseRequired(let salt):
                        PassphraseView(salt: salt, isFailed: false) { passphrase in
                            viewModel.submitPassphrase(passphrase, salt: salt)
                        }

                    case .failed(let salt):
                        PassphraseView(salt: salt, isFailed: true) { passphrase in
                            viewModel.submitPassphrase(passphrase, salt: salt)
                        }

                    default:
                        TerminalRepresentable(viewModel: viewModel)
                            .ignoresSafeArea(.keyboard, edges: .bottom)
                    }
```

- [ ] **Step 2: Add encrypted indicator to status bar**

In the `statusBar` computed property, add an encrypted badge after the mode badge (after line 121):

```swift
                if viewModel.joinedInfo?.encrypted == true {
                    Image(systemName: "lock.fill")
                        .font(.system(size: 10))
                        .foregroundStyle(PhosphorTheme.green)
                }
```

- [ ] **Step 3: Verify it compiles**

```bash
xcodebuild -project ios/Phosphor.xcodeproj -scheme Phosphor -destination 'generic/platform=iOS' build 2>&1 | tail -5
```
Expected: BUILD SUCCEEDED

- [ ] **Step 4: Commit**

```bash
git add ios/Phosphor/Views/TerminalContainerView.swift
git commit -m "feat(ios): integrate PassphraseView into TerminalContainerView"
```

---

### Task 10: Manual E2E Verification

This task verifies the full flow end-to-end using a local relay.

- [ ] **Step 1: Start local relay**

```bash
cd /Users/brporter/projects/phosphor
go run ./cmd/relay
```

- [ ] **Step 2: Start encrypted CLI session**

In a second terminal:
```bash
go run ./cmd/phosphor --relay ws://localhost:8080 --key "test123" -- bash
```

Note the session ID from the output.

- [ ] **Step 3: Test iOS app**

Run the iOS app in the simulator. Point it at `http://localhost:8080`. Join the session. Verify:
1. PassphraseView appears with "Encrypted Session" title
2. Enter wrong passphrase → error message appears
3. Enter correct passphrase "test123" → terminal appears with session output
4. Type in terminal → keystrokes reach the CLI
5. Lock icon appears in the status bar

- [ ] **Step 4: Test unencrypted session**

Start a CLI without `--key`:
```bash
go run ./cmd/phosphor --relay ws://localhost:8080 -- bash
```

Join from iOS → terminal should appear immediately with no passphrase prompt.

- [ ] **Step 5: Commit any fixes discovered during testing**
