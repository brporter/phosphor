import Foundation

struct SessionData: Codable, Identifiable {
    let id: String
    let mode: String
    let cols: Int
    let rows: Int
    let command: String
    let viewers: Int
    let processExited: Bool

    enum CodingKeys: String, CodingKey {
        case id, mode, cols, rows, command, viewers
        case processExited = "process_exited"
    }
}
