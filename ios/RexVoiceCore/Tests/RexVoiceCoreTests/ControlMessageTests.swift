import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct ControlMessageTests {
    // JSON text as written in docs/protocol.md section 3 (placeholders filled in).
    static let cases: [(ControlMessage, String)] = [
        (.hello(app: "1.0", device: "iPhone17,1", deviceID: "6F1C"),
         #"{"t":"hello","app":"1.0","device":"iPhone17,1","device_id":"6F1C"}"#),
        (.callStart(provider: nil, model: nil, voice: nil, apiKey: nil), #"{"t":"call.start"}"#),
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

    @Test func callStartOmitsNilFields() throws {
        let msg = ControlMessage.callStart(provider: "qwen", model: "qwen3.8-omni-flash-realtime", voice: nil, apiKey: "sk-1234")
        #expect(try Self.object(JSONEncoder().encode(msg))
            == Self.object(Data(#"{"t":"call.start","provider":"qwen","model":"qwen3.8-omni-flash-realtime","api_key":"sk-1234"}"#.utf8)))
    }

    @Test func callStartDecodesOldAndNewForms() throws {
        let old = try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"t":"call.start"}"#.utf8))
        #expect(old == .callStart(provider: nil, model: nil, voice: nil, apiKey: nil))
        let new = try JSONDecoder().decode(ControlMessage.self, from: Data(
            #"{"t":"call.start","provider":"grok","model":"grok-voice-3.0","voice":"ara","api_key":"xai-1"}"#.utf8))
        #expect(new == .callStart(provider: "grok", model: "grok-voice-3.0", voice: "ara", apiKey: "xai-1"))
    }

    @Test func voicePreviewOmitsNilFields() throws {
        let full = ControlMessage.voicePreview(provider: "openai", model: "gpt-realtime-2.1", voice: "marin", apiKey: "sk-9")
        #expect(try Self.object(JSONEncoder().encode(full)) == Self.object(Data(
            #"{"t":"voice.preview","provider":"openai","model":"gpt-realtime-2.1","voice":"marin","api_key":"sk-9"}"#.utf8)))
        let bare = ControlMessage.voicePreview(provider: "qwen", model: nil, voice: "Tina", apiKey: nil)
        #expect(try Self.object(JSONEncoder().encode(bare))
            == Self.object(Data(#"{"t":"voice.preview","provider":"qwen","voice":"Tina"}"#.utf8)))
    }

    @Test func previewDecodes() throws {
        let playing = try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"t":"preview","state":"playing"}"#.utf8))
        #expect(playing == .preview(state: "playing", msg: nil))
        let failed = try JSONDecoder().decode(ControlMessage.self, from: Data(#"{"t":"preview","state":"error","msg":"已取消"}"#.utf8))
        #expect(failed == .preview(state: "error", msg: "已取消"))
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
