import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct ControlMessageTests {
    // JSON text as written in docs/protocol.md section 3 (placeholders filled in).
    static let cases: [(ControlMessage, String)] = [
        (.hello(app: "1.0", device: "iPhone17,1", deviceID: "6F1C"),
         #"{"t":"hello","app":"1.0","device":"iPhone17,1","device_id":"6F1C"}"#),
        (.callStart, #"{"t":"call.start"}"#),
        (.callEnd, #"{"t":"call.end"}"#),
        (.status(state: "listening", detail: "已接通"), #"{"t":"status","state":"listening","detail":"已接通"}"#),
        (.status(state: "ended", detail: nil), #"{"t":"status","state":"ended"}"#), // what internal/call sends
        (.transcript(who: "assistant", text: "好的"), #"{"t":"transcript","who":"assistant","text":"好的"}"#),
        (.interrupt, #"{"t":"interrupt"}"#),
        (.hangUp(reason: "idle"), #"{"t":"hang_up","reason":"idle"}"#),
        (.error(msg: "换手机要重新 pair"), #"{"t":"error","msg":"换手机要重新 pair"}"#),
    ]

    static func object(_ json: Data) throws -> NSDictionary {
        try #require(JSONSerialization.jsonObject(with: json) as? NSDictionary)
    }

    @Test(arguments: 0..<cases.count) func encodesAsProtocol(i: Int) throws {
        let (msg, text) = Self.cases[i]
        let encoded = try JSONEncoder().encode(msg)
        #expect(try Self.object(encoded) == Self.object(Data(text.utf8)))
        #expect(try JSONDecoder().decode(ControlMessage.self, from: Data(text.utf8)) == msg)
        #expect(try JSONDecoder().decode(ControlMessage.self, from: encoded) == msg)
    }

    @Test func unknownTypeDoesNotThrow() throws {
        let msg = try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"t":"ping","x":1}"#.utf8))
        #expect(msg == .unknown("ping"))
    }

    @Test func missingFieldThrows() {
        #expect(throws: DecodingError.self) {
            try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"t":"transcript","who":"user"}"#.utf8))
        }
        #expect(throws: DecodingError.self) {
            try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"state":"x"}"#.utf8))
        }
    }
}
