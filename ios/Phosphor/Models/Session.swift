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

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        mode = try container.decode(String.self, forKey: .mode)
        cols = try container.decode(Int.self, forKey: .cols)
        rows = try container.decode(Int.self, forKey: .rows)
        command = try container.decode(String.self, forKey: .command)
        viewers = try container.decode(Int.self, forKey: .viewers)
        processExited = try container.decodeIfPresent(Bool.self, forKey: .processExited) ?? false
    }
}
