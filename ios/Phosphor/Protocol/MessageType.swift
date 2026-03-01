import Foundation

/// Binary message type bytes matching internal/protocol/messages.go
enum MessageType: UInt8 {
    case stdout      = 0x01
    case stdin       = 0x02
    case resize      = 0x03
    case hello       = 0x10
    case welcome     = 0x11
    case join        = 0x12
    case joined      = 0x13
    case reconnect   = 0x14
    case end         = 0x15
    case error          = 0x16
    case processExited  = 0x17
    case restart        = 0x18
    case viewerCount    = 0x20
    case mode        = 0x21
    case ping        = 0x30
    case pong        = 0x31
}
