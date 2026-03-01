import SwiftUI

struct SessionCardView: View {
    let session: SessionData

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                // Command name
                Text(session.command)
                    .font(.system(size: 15, weight: .semibold, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.green)
                    .lineLimit(1)

                Spacer()

                // Mode badge
                Text(session.mode.uppercased())
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.background)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(PhosphorTheme.amber)
                    .clipShape(RoundedRectangle(cornerRadius: 4))
            }

            HStack(spacing: 16) {
                // Terminal size
                Label("\(session.cols)x\(session.rows)", systemImage: "rectangle.split.3x3")
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text)

                // Viewer count
                Label("\(session.viewers)", systemImage: "eye")
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text)

                Spacer()

                // Session ID
                Text(session.id)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(PhosphorTheme.text.opacity(0.5))
            }
        }
        .padding(12)
        .background(PhosphorTheme.card)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(PhosphorTheme.border, lineWidth: 1)
        )
    }
}
