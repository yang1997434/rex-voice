import Foundation

/// First byte of every phone <-> Mac frame (docs/protocol.md section 3).
public enum FrameType: UInt8, Sendable {
    case audio = 0, control = 1, salt = 2, keepalive = 3
}

/// Plaintext session-salt frame `[02][00000000][16-byte salt]`.
public enum SaltFrame {
    public static func make(_ salt: Data) -> Data {
        precondition(salt.count == 16, "session salt must be 16 bytes")
        return Data([FrameType.salt.rawValue, 0, 0, 0, 0]) + salt
    }

    /// Accepts only an exact 21-byte type 2 frame with a zero seq field, like the Mac side.
    public static func parse(_ frame: Data) -> Data? {
        let f = Data(frame) // rebase so indices start at 0
        guard f.count == 21, f[0] == FrameType.salt.rawValue, f[1..<5].allSatisfy({ $0 == 0 }) else { return nil }
        return Data(f[5...])
    }
}

/// Plaintext keepalive frame `[03][00000000]`, sent every 30 s while idle.
public enum KeepaliveFrame {
    public static let data = Data([FrameType.keepalive.rawValue, 0, 0, 0, 0])
}
