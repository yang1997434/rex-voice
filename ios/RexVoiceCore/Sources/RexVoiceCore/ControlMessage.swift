import Foundation

/// JSON body of a type 1 frame, discriminated by `t` (docs/protocol.md section 3).
/// String fields stay plain strings so a new Mac-side value never breaks decoding.
public enum ControlMessage: Codable, Equatable, Sendable {
    // phone -> Mac
    case hello(app: String, device: String, deviceID: String)
    /// All optional; nil fields are left out of the JSON and the Mac falls back to its own defaults.
    /// `name` is how the assistant addresses the user, replacing the Mac config's `name` for this call.
    case callStart(provider: String?, model: String?, voice: String?, apiKey: String?, name: String? = nil)
    case callEnd
    /// Voice sample outside a call; same optional fields as `callStart`.
    case voicePreview(provider: String?, model: String?, voice: String?, apiKey: String?, name: String? = nil)
    // Mac -> phone
    /// `detail` is optional: the Mac side currently sends status without it.
    case status(state: String, detail: String?)
    case transcript(who: String, text: String)
    case interrupt
    case hangUp(reason: String)
    case error(msg: String)
    /// Voice sample progress: connecting / playing / done / error (`msg` set on error).
    case preview(state: String, msg: String?)
    /// An image follows in `chunks` type 4 frames carrying this `id`; `size` is the whole file in bytes.
    case attach(id: Int, name: String, mime: String, size: Int, chunks: Int)
    case link(url: String, title: String)
    /// Any `t` this build does not know; decoding never throws for it.
    case unknown(String)

    private enum Key: String, CodingKey {
        case t, app, device, deviceID = "device_id", state, detail, who, text, reason, msg
        case provider, model, voice, apiKey = "api_key"
        case id, name, mime, size, chunks, url, title
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        let t = try c.decode(String.self, forKey: .t)
        switch t {
        case "hello":
            self = .hello(app: try c.decode(String.self, forKey: .app),
                          device: try c.decode(String.self, forKey: .device),
                          deviceID: try c.decode(String.self, forKey: .deviceID))
        case "call.start":
            self = .callStart(provider: try c.decodeIfPresent(String.self, forKey: .provider),
                              model: try c.decodeIfPresent(String.self, forKey: .model),
                              voice: try c.decodeIfPresent(String.self, forKey: .voice),
                              apiKey: try c.decodeIfPresent(String.self, forKey: .apiKey),
                              name: try c.decodeIfPresent(String.self, forKey: .name))
        case "call.end": self = .callEnd
        case "voice.preview":
            self = .voicePreview(provider: try c.decodeIfPresent(String.self, forKey: .provider),
                                 model: try c.decodeIfPresent(String.self, forKey: .model),
                                 voice: try c.decodeIfPresent(String.self, forKey: .voice),
                                 apiKey: try c.decodeIfPresent(String.self, forKey: .apiKey),
                                 name: try c.decodeIfPresent(String.self, forKey: .name))
        case "status":
            self = .status(state: try c.decode(String.self, forKey: .state),
                           detail: try c.decodeIfPresent(String.self, forKey: .detail))
        case "transcript":
            self = .transcript(who: try c.decode(String.self, forKey: .who), text: try c.decode(String.self, forKey: .text))
        case "interrupt": self = .interrupt
        case "hang_up": self = .hangUp(reason: try c.decode(String.self, forKey: .reason))
        case "error": self = .error(msg: try c.decode(String.self, forKey: .msg))
        case "preview":
            self = .preview(state: try c.decode(String.self, forKey: .state), msg: try c.decodeIfPresent(String.self, forKey: .msg))
        case "attach":
            self = .attach(id: try c.decode(Int.self, forKey: .id), name: try c.decode(String.self, forKey: .name),
                           mime: try c.decode(String.self, forKey: .mime), size: try c.decode(Int.self, forKey: .size),
                           chunks: try c.decode(Int.self, forKey: .chunks))
        case "link":
            self = .link(url: try c.decode(String.self, forKey: .url), title: try c.decode(String.self, forKey: .title))
        default: self = .unknown(t)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: Key.self)
        switch self {
        case let .hello(app, device, deviceID):
            try c.encode("hello", forKey: .t)
            try c.encode(app, forKey: .app)
            try c.encode(device, forKey: .device)
            try c.encode(deviceID, forKey: .deviceID)
        case let .callStart(provider, model, voice, apiKey, name):
            try c.encode("call.start", forKey: .t)
            try c.encodeIfPresent(provider, forKey: .provider)
            try c.encodeIfPresent(model, forKey: .model)
            try c.encodeIfPresent(voice, forKey: .voice)
            try c.encodeIfPresent(apiKey, forKey: .apiKey)
            try c.encodeIfPresent(name, forKey: .name)
        case .callEnd: try c.encode("call.end", forKey: .t)
        case let .voicePreview(provider, model, voice, apiKey, name):
            try c.encode("voice.preview", forKey: .t)
            try c.encodeIfPresent(provider, forKey: .provider)
            try c.encodeIfPresent(model, forKey: .model)
            try c.encodeIfPresent(voice, forKey: .voice)
            try c.encodeIfPresent(apiKey, forKey: .apiKey)
            try c.encodeIfPresent(name, forKey: .name)
        case let .status(state, detail):
            try c.encode("status", forKey: .t)
            try c.encode(state, forKey: .state)
            try c.encodeIfPresent(detail, forKey: .detail)
        case let .transcript(who, text):
            try c.encode("transcript", forKey: .t)
            try c.encode(who, forKey: .who)
            try c.encode(text, forKey: .text)
        case .interrupt: try c.encode("interrupt", forKey: .t)
        case let .hangUp(reason):
            try c.encode("hang_up", forKey: .t)
            try c.encode(reason, forKey: .reason)
        case let .error(msg):
            try c.encode("error", forKey: .t)
            try c.encode(msg, forKey: .msg)
        case let .preview(state, msg):
            try c.encode("preview", forKey: .t)
            try c.encode(state, forKey: .state)
            try c.encodeIfPresent(msg, forKey: .msg)
        case let .attach(id, name, mime, size, chunks):
            try c.encode("attach", forKey: .t)
            try c.encode(id, forKey: .id)
            try c.encode(name, forKey: .name)
            try c.encode(mime, forKey: .mime)
            try c.encode(size, forKey: .size)
            try c.encode(chunks, forKey: .chunks)
        case let .link(url, title):
            try c.encode("link", forKey: .t)
            try c.encode(url, forKey: .url)
            try c.encode(title, forKey: .title)
        case let .unknown(t):
            try c.encode(t, forKey: .t)
        }
    }
}
