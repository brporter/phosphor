import SwiftUI

struct SettingsView: View {
    let auth: AuthViewModel

    @AppStorage("relay_url") private var relayURL = "https://phosphor.betaporter.dev"
    private let defaultURL = "https://phosphor.betaporter.dev"

    var body: some View {
        ZStack {
            PhosphorTheme.background.ignoresSafeArea()

            Form {
                Section {
                    TextField("Relay URL", text: $relayURL)
                        .font(.system(size: 14, design: .monospaced))
                        #if !os(macOS)
                        .textInputAutocapitalization(.never)
                        #endif
                        .autocorrectionDisabled()
                        .foregroundStyle(PhosphorTheme.textBright)

                    if relayURL != defaultURL {
                        Button("Reset to default") {
                            relayURL = defaultURL
                        }
                        .foregroundStyle(PhosphorTheme.amber)
                    }
                } header: {
                    Text("Relay Server")
                        .foregroundStyle(PhosphorTheme.text)
                } footer: {
                    Text("The WebSocket relay server that hosts terminal sessions.")
                        .foregroundStyle(PhosphorTheme.text.opacity(0.6))
                }

                Section {
                    if let user = auth.user {
                        LabeledContent("Subject") {
                            Text(user.sub)
                                .font(.system(size: 12, design: .monospaced))
                                .foregroundStyle(PhosphorTheme.text)
                        }

                        if let email = user.email {
                            LabeledContent("Email") {
                                Text(email)
                                    .font(.system(size: 12, design: .monospaced))
                                    .foregroundStyle(PhosphorTheme.text)
                            }
                        }

                        LabeledContent("Issuer") {
                            Text(user.iss)
                                .font(.system(size: 11, design: .monospaced))
                                .foregroundStyle(PhosphorTheme.text)
                                .lineLimit(1)
                        }

                        Button("Sign Out") {
                            auth.logout()
                        }
                        .foregroundStyle(PhosphorTheme.red)
                    }
                } header: {
                    Text("Account")
                        .foregroundStyle(PhosphorTheme.text)
                }
            }
            .scrollContentBackground(.hidden)
        }
        .navigationTitle("Settings")
        #if !os(macOS)
        .toolbarColorScheme(.dark, for: .navigationBar)
        #endif
    }
}
