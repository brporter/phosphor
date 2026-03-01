import AuthenticationServices
import SwiftUI

struct LoginView: View {
    let auth: AuthViewModel

    @AppStorage("relay_url") private var relayURL = "https://phosphor.betaporter.dev"
    @State private var isEditingRelay = false
    @State private var editedURL = ""

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()

            VStack(spacing: 32) {
                Spacer()

                // Logo
                VStack(spacing: 8) {
                    Text("PHOSPHOR")
                        .font(.system(size: 36, weight: .bold, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.green)

                    Text("terminal sharing")
                        .font(.system(size: 14, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.text)
                }

                Spacer()

                // Sign-in buttons
                VStack(spacing: 12) {
                    SignInButton(
                        title: "Sign in with Microsoft",
                        icon: "person.badge.shield.checkmark.fill",
                        auth: auth,
                        provider: "microsoft"
                    )

                    SignInButton(
                        title: "Sign in with Google",
                        icon: "globe",
                        auth: auth,
                        provider: "google"
                    )

                    SignInButton(
                        title: "Sign in with Apple",
                        icon: "apple.logo",
                        auth: auth,
                        provider: "apple"
                    )
                }
                .padding(.horizontal, 32)

                if auth.isLoading {
                    ProgressView()
                        .tint(PhosphorTheme.green)
                }

                if let error = auth.error {
                    Text(error)
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(PhosphorTheme.red)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 32)
                }

                Spacer()

                // Relay URL
                Button {
                    editedURL = relayURL
                    isEditingRelay = true
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "server.rack")
                            .font(.system(size: 11))
                        Text(relayURL)
                            .font(.system(size: 12, design: .monospaced))
                            .lineLimit(1)
                    }
                    .foregroundStyle(PhosphorTheme.text.opacity(0.5))
                }
                .padding(.bottom, 16)
            }
        }
        .alert("Relay Server", isPresented: $isEditingRelay) {
            TextField("https://relay.example.com", text: $editedURL)
                #if !os(macOS)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
            Button("Save") {
                let trimmed = editedURL.trimmingCharacters(in: .whitespacesAndNewlines)
                if !trimmed.isEmpty {
                    relayURL = trimmed
                }
            }
            Button("Reset to Default", role: .destructive) {
                relayURL = "https://phosphor.betaporter.dev"
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Enter the URL of your Phosphor relay server.")
        }
    }
}

private struct SignInButton: View {
    let title: String
    let icon: String
    let auth: AuthViewModel
    let provider: String

    var body: some View {
        Button {
            guard let window = getKeyWindow() else { return }
            Task {
                await auth.login(provider: provider, anchor: window)
            }
        } label: {
            HStack(spacing: 12) {
                Image(systemName: icon)
                    .frame(width: 20)
                Text(title)
                    .font(.system(size: 15, weight: .medium, design: .monospaced))
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 14)
            .background(PhosphorTheme.card)
            .foregroundStyle(PhosphorTheme.textBright)
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(PhosphorTheme.border, lineWidth: 1)
            )
        }
        .disabled(auth.isLoading)
    }

    private func getKeyWindow() -> ASPresentationAnchor? {
        #if os(iOS) || targetEnvironment(macCatalyst)
        return UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first { $0.isKeyWindow }
        #elseif os(macOS)
        return NSApplication.shared.keyWindow
        #endif
    }
}

