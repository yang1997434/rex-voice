import CryptoKit
import Foundation

public enum FrameCipherError: Error, Equatable, Sendable {
    /// seq not greater than the last accepted one.
    case replay
    /// Too short, unknown type byte, or failed authentication.
    case badFrame
    /// Sender reached seq 2^32-1; sending more would reuse a nonce. Reconnect to get fresh salts.
    case seqExhausted
}

/// Seals or opens frames for one direction of one connection (docs/protocol.md section 3).
/// Use one instance per direction per side; the internal lock makes it safe to share across tasks.
public final class FrameCipher: @unchecked Sendable {
    public enum Direction: UInt32, Sendable {
        case phoneToMac = 0, macToPhone = 1
    }

    private let key: SymmetricKey
    private let direction: Direction
    private let lock = NSLock()
    /// Seal: last sealed seq. Open: last accepted seq. First frame is seq 1. Guarded by `lock`.
    var seq: UInt32 = 0

    public init(key: SymmetricKey, direction: Direction, phoneSalt: Data, macSalt: Data) {
        precondition(phoneSalt.count == 16 && macSalt.count == 16, "session salts must be 16 bytes")
        self.key = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: key,
            salt: phoneSalt + macSalt,
            info: Data((direction == .phoneToMac ? "phone->mac" : "mac->phone").utf8),
            outputByteCount: 32)
        self.direction = direction
    }

    private func nonce(_ seq: UInt32) -> ChaChaPoly.Nonce {
        var n = Data(count: 4)
        withUnsafeBytes(of: direction.rawValue.bigEndian) { n.append(contentsOf: $0) }
        withUnsafeBytes(of: seq.bigEndian) { n.append(contentsOf: $0) }
        return try! ChaChaPoly.Nonce(data: n) // always 12 bytes, cannot fail
    }

    /// Returns `[type][seq BE32][ciphertext ‖ tag]`; the nonce is not on the wire.
    public func seal(type: FrameType, _ plain: Data) throws -> Data {
        try lock.withLock {
            guard seq < .max else { throw FrameCipherError.seqExhausted }
            seq += 1
            var header = Data([type.rawValue])
            withUnsafeBytes(of: seq.bigEndian) { header.append(contentsOf: $0) }
            let box = try ChaChaPoly.seal(plain, using: key, nonce: nonce(seq), authenticating: header.prefix(1))
            return header + box.ciphertext + box.tag
        }
    }

    /// Verifies and decrypts a frame; the last accepted seq only advances on success.
    public func open(_ frame: Data) throws -> (type: FrameType, seq: UInt32, plain: Data) {
        let f = Data(frame) // rebase so indices start at 0
        guard f.count >= 5 + 16, let type = FrameType(rawValue: f[0]) else { throw FrameCipherError.badFrame }
        let frameSeq = f[1..<5].reduce(UInt32(0)) { $0 << 8 | UInt32($1) }
        return try lock.withLock {
            guard frameSeq > seq else { throw FrameCipherError.replay }
            let plain: Data
            do {
                let box = try ChaChaPoly.SealedBox(
                    nonce: nonce(frameSeq), ciphertext: f[5..<(f.count - 16)], tag: f.suffix(16))
                plain = try ChaChaPoly.open(box, using: key, authenticating: f.prefix(1))
            } catch {
                throw FrameCipherError.badFrame
            }
            seq = frameSeq
            return (type, frameSeq, plain)
        }
    }
}
