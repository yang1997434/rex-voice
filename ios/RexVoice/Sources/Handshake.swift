import CryptoKit
import Foundation
import RexVoiceCore

/// Phone side of the per-connection salt exchange (docs/protocol.md section 3). Pure state, no I/O,
/// so the rules can be checked without a network.
struct Handshake {
    let key: SymmetricKey
    private(set) var phoneSalt = Handshake.newSalt()
    private(set) var macSalt: Data?
    private(set) var tx: FrameCipher?
    private(set) var rx: FrameCipher?

    init(key: SymmetricKey) { self.key = key }

    var ready: Bool { tx != nil }

    /// A new WebSocket connection: fresh salt, forget the Mac. Returns the salt frame to send.
    mutating func connected() -> Data {
        phoneSalt = Self.newSalt()
        forgetMac()
        return SaltFrame.make(phoneSalt)
    }

    /// Relay `peer` notice. Returns the salt frame to send, if any. While no Mac salt has arrived the
    /// same phone salt is resent (the relay may report present:true twice); otherwise a new one.
    mutating func peer(present: Bool) -> Data? {
        guard present else {
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
    mutating func mac(salt: Data) -> Bool {
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

    static func newSalt() -> Data {
        SymmetricKey(size: .init(bitCount: 128)).withUnsafeBytes { Data($0) }
    }
}
