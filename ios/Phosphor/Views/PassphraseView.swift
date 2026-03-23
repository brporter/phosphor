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
        isFocused = true
        onSubmit(submitted)
    }
}
