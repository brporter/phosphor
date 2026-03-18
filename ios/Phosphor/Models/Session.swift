import Foundation

struct SessionData: Codable, Identifiable {
    let id: String
    let mode: String
    let cols: Int
    let rows: Int
    let command: String
    let hostname: String
    let viewers: Int
    let processExited: Bool
    let lazy: Bool
    let processRunning: Bool

    enum CodingKeys: String, CodingKey {
        case id, mode, cols, rows, command, hostname, viewers, lazy
        case processExited = "process_exited"
        case processRunning = "process_running"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        mode = try container.decode(String.self, forKey: .mode)
        cols = try container.decode(Int.self, forKey: .cols)
        rows = try container.decode(Int.self, forKey: .rows)
        command = try container.decode(String.self, forKey: .command)
        hostname = try container.decodeIfPresent(String.self, forKey: .hostname) ?? ""
        viewers = try container.decode(Int.self, forKey: .viewers)
        processExited = try container.decodeIfPresent(Bool.self, forKey: .processExited) ?? false
        lazy = try container.decodeIfPresent(Bool.self, forKey: .lazy) ?? false
        processRunning = try container.decodeIfPresent(Bool.self, forKey: .processRunning) ?? false
    }
}
