import CryptoKit
import Foundation

public enum PairingError: Error, Equatable, Sendable {
    case badScheme, badRelay, badID, badKey
}

/// What the QR code carries (docs/protocol.md section 1).
public struct Pairing: Sendable {
    public let id: Data
    public let key: SymmetricKey
    public let relay: URL

    public init(id: Data, key: SymmetricKey, relay: URL) {
        self.id = id
        self.key = key
        self.relay = relay
    }

    /// Parses `rexvoice://pair?relay=<ws(s) URL>&id=<b64url>&key=<b64url>`; id is 16 bytes, key 32, both unpadded base64url.
    public static func parse(_ url: URL) throws -> Pairing {
        guard url.scheme == "rexvoice", url.host() == "pair",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else { throw PairingError.badScheme }
        func value(_ name: String) -> String? { items.first { $0.name == name }?.value }

        guard let relayText = value("relay"), let relay = URL(string: relayText),
              relay.scheme == "wss" || relay.scheme == "ws", relay.host()?.isEmpty == false
        else { throw PairingError.badRelay }
        guard let id = value("id").flatMap(base64URLDecode), id.count == 16 else { throw PairingError.badID }
        guard let key = value("key").flatMap(base64URLDecode), key.count == 32 else { throw PairingError.badKey }
        return Pairing(id: id, key: SymmetricKey(data: key), relay: relay)
    }
}

/// Unpadded base64url only: any `=`, `+`, `/` or other stray character is rejected.
func base64URLDecode(_ s: String) -> Data? {
    let allowed = Set("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")
    guard s.allSatisfy(allowed.contains), s.count % 4 != 1 else { return nil }
    let std = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
    return Data(base64Encoded: std + String(repeating: "=", count: (4 - std.count % 4) % 4))
}
