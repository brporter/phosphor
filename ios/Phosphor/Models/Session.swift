import Foundation

struct SessionData: Codable, Identifiable {
    let id: String
    let mode: String
    let cols: Int
    let rows: Int
    let command: String
    let viewers: Int
}
