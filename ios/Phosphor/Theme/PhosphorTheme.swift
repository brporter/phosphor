import SwiftUI

enum PhosphorTheme {
    // Background shades
    static let background = Color(hex: 0x0A0A0A)
    static let panel = Color(hex: 0x111111)
    static let card = Color(hex: 0x1A1A1A)

    // Accent colors
    static let green = Color(hex: 0x00FF41)
    static let amber = Color(hex: 0xFFB000)
    static let red = Color(hex: 0xFF3333)
    static let cyan = Color(hex: 0x00E5FF)

    // Text
    static let text = Color(hex: 0xB0B0B0)
    static let textBright = Color(hex: 0xE0E0E0)

    // Borders
    static let border = Color(hex: 0x333333)

    static let terminalFont: Font = .system(size: 14, design: .monospaced)
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
