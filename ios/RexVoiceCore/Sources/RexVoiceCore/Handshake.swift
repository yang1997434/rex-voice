import CryptoKit
import Foundation

/// Phone side of the per-connection salt exchange (docs/protocol.md section 3). Pure state, no I/O,
/// so the rules can be checked without a network.
public struct Handshake: Sendable {
    public let key: SymmetricKey
    public private(set) var phoneSalt = Handshake.newSalt()
    public private(set) var macSalt: Data?
    public private(set) var tx: FrameCipher?
    public private(set) var rx: FrameCipher?

    public init(key: SymmetricKey) { self.key = key }

    public var ready: Bool { tx != nil }

    /// A new WebSocket connection: fresh salt, forget the Mac. Returns the salt frame to send.
    public mutating func connected() -> Data {
        phoneSalt = Self.newSalt()
        forgetMac()
        return SaltFrame.make(phoneSalt)
    }

    /// Relay `peer` notice. Returns the salt frame to send, if any. While no Mac salt has arrived the
    /// same phone salt is resent (the relay may report present:true twice); otherwise a new one.
    public mutating func peer(present: Bool) -> Data? {
        guard present else {
            phoneSalt = Self.newSalt() // fresh keys next time even if the Mac reused its salt
            forgetMac()
            return nil
        }
        if macSalt != nil {
            phoneSalt = Self.newSalt()
            forgetMac()
        }
        return SaltFrame.make(phoneSalt)
    }

    /// A Mac salt arrived. Returns true when keys were (re)derived, i.e. `hello` must be sent now.
    /// A repeated Mac salt is ignored, else seq would restart under the same keys.
    public mutating func mac(salt: Data) -> Bool {
        guard salt != macSalt else { return false }
        macSalt = salt
        tx = FrameCipher(key: key, direction: .phoneToMac, phoneSalt: phoneSalt, macSalt: salt)
        rx = FrameCipher(key: key, direction: .macToPhone, phoneSalt: phoneSalt, macSalt: salt)
        return true
    }

    private mutating func forgetMac() {
        macSalt = nil
        tx = nil
        rx = nil
    }

    public static func newSalt() -> Data {
        SymmetricKey(size: .init(bitCount: 128)).withUnsafeBytes { Data($0) }
    }
}
