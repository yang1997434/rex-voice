import CryptoKit
import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct PairingTests {
    // Shape produced by Go pair.URL: relay is query-escaped, id/key unpadded base64url.
    static let id = "AAECAwQFBgcICQoLDA0ODw" // 0x00...0x0f
    static let key = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" // 0x00...0x1f
    static let relay = "wss%3A%2F%2Fr.maruiao.top%2F_voice%2Fws"

    @Test func parsesGoURL() throws {
        let p = try Pairing.parse(URL(string: "rexvoice://pair?relay=\(Self.relay)&id=\(Self.id)&key=\(Self.key)")!)
        #expect(p.relay.absoluteString == "wss://r.maruiao.top/_voice/ws")
        #expect(p.id == Data((0...15).map { UInt8($0) }))
        #expect(p.key.withUnsafeBytes { Data($0) } == Data((0...31).map { UInt8($0) }))
        #expect(p.invite == nil)
    }

    @Test func parsesInvite() throws {
        let p = try Pairing.parse(URL(string: "rexvoice://pair?relay=\(Self.relay)&id=\(Self.id)&key=\(Self.key)&invite=test-code_1")!)
        #expect(p.invite == "test-code_1")
        #expect(p.relay.absoluteString == "wss://r.maruiao.top/_voice/ws")
    }

    @Test func parsesURLSafeAlphabet() throws {
        // 0xfb 0xff ... encodes to '-' and '_' in base64url.
        let id = Data(repeating: 0xfb, count: 15) + Data([0xff])
        let b64 = id.base64EncodedString().replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
        #expect(b64.contains("-") || b64.contains("_"))
        let p = try Pairing.parse(URL(string: "rexvoice://pair?relay=\(Self.relay)&id=\(b64)&key=\(Self.key)")!)
        #expect(p.id == id)
    }

    @Test(arguments: [
        ("https://pair?relay=\(relay)&id=\(id)&key=\(key)", PairingError.badScheme),
        ("rexvoice://other?relay=\(relay)&id=\(id)&key=\(key)", .badScheme),
        ("rexvoice://pair?id=\(id)&key=\(key)", .badRelay),
        ("rexvoice://pair?relay=https%3A%2F%2Fx.com&id=\(id)&key=\(key)", .badRelay),
        ("rexvoice://pair?relay=\(relay)&key=\(key)", .badID),
        ("rexvoice://pair?relay=\(relay)&id=AAECAwQFBgcICQoLDA0O&key=\(key)", .badID), // 15 bytes
        ("rexvoice://pair?relay=\(relay)&id=\(id)", .badKey), // missing key
        ("rexvoice://pair?relay=\(relay)&id=\(id)&key=\(key)%3D", .badKey), // padded
        ("rexvoice://pair?relay=\(relay)&id=\(id)&key=AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8%2B", .badKey), // '+'
    ])
    func rejects(url: String, want: PairingError) {
        #expect(throws: want) { try Pairing.parse(URL(string: url)!) }
    }
}
