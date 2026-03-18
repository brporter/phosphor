import SwiftUI

enum PhosphorTheme {
    // Background shades — matched to web SPA CRT theme
    static let background = Color(hex: 0x050808)
    static let panel = Color(hex: 0x0A1A0A)
    static let card = Color(hex: 0x0A120A)

    // Accent colors
    static let green = Color(hex: 0x00FF41)
    static let greenDim = Color(hex: 0x00CC33)
    static let greenSubtle = Color(hex: 0x00AA33)
    static let amber = Color(hex: 0xFFB000)
    static let red = Color(hex: 0xFF3333)
    static let cyan = Color(hex: 0x00E5FF)

    // Text
    static let text = Color(hex: 0xB0B0B0)
    static let textBright = Color(hex: 0xE0E0E0)
    static let textDim = Color(hex: 0x1A8A1A)

    // Borders
    static let border = Color(hex: 0x0A3A0A)

    static let terminalFont: Font = .system(size: 14, design: .monospaced)
}

// MARK: - CRT Visual Effects

struct ScanlineOverlay: View {
    var body: some View {
        Canvas { context, size in
            // Draw subtle horizontal scanlines
            let lineSpacing: CGFloat = 2
            var y: CGFloat = 0
            while y < size.height {
                let rect = CGRect(x: 0, y: y, width: size.width, height: 1)
                context.fill(Path(rect), with: .color(Color(hex: 0x00FF41, opacity: 0.015)))
                y += lineSpacing
            }
        }
        .allowsHitTesting(false)
    }
}

extension View {
    /// Adds a subtle green glow text shadow effect
    func glowText(color: Color = PhosphorTheme.green, radius: CGFloat = 6, opacity: Double = 0.3) -> some View {
        self.shadow(color: color.opacity(opacity), radius: radius)
    }

    /// Adds a CRT-style inset glow to a card
    func crtCardStyle() -> some View {
        self
            .background(PhosphorTheme.card)
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(PhosphorTheme.border, lineWidth: 1)
            )
            .shadow(color: PhosphorTheme.green.opacity(0.02), radius: 10, x: 0, y: 0)
    }
}

extension Color {
    init(hex: UInt32, opacity: Double = 1.0) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255.0,
            green: Double((hex >> 8) & 0xFF) / 255.0,
            blue: Double(hex & 0xFF) / 255.0,
            opacity: opacity
        )
    }
}
