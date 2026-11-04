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
    /// Answers a blocked Claude pane without a call: `keys` (send_keys whitelist, e.g. ["2"]) or `text` (send_to_pane).
    case answer(block: String, keys: [String]?, text: String?)
    /// `push:false` stops the Mac's lock-screen notify (ask cards still come). Mac keeps it in memory only: resent after every hello.
    case prefs(push: Bool)
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
    /// A Claude pane turned blocked; `options[i]` is picked by key i+1 (may be empty). Resent after every hello.
    case ask(block: String, session: String, project: String, msg: String, options: [String])
    /// That pane is no longer blocked.
    case askDone(block: String)
    /// Outcome of an `answer`; `msg` says why when `ok` is false.
    case answerResult(block: String, ok: Bool, msg: String?)
    /// Running total of the current call: `usd` spent so far, `seconds` since it started.
    case cost(usd: Double, seconds: Double)
    /// Mac-side usage totals, sent after every hello.
    case stats(Stats)
    /// Any `t` this build does not know; decoding never throws for it.
    case unknown(String)

    private enum Key: String, CodingKey {
        case t, app, device, deviceID = "device_id", state, detail, who, text, reason, msg
        case provider, model, voice, apiKey = "api_key"
        case id, name, mime, size, chunks, url, title
        case block, session, project, options, keys, ok, push
        case usd, seconds
    }

    public struct Stats: Codable, Equatable, Sendable {
        public struct Bucket: Codable, Equatable, Sendable {
            public var calls: Int
            public var seconds: Double
            public var usd: Double
            public init(calls: Int, seconds: Double, usd: Double) { self.calls = calls; self.seconds = seconds; self.usd = usd }
        }
        public var today: Bucket
        public var week: Bucket
        public var month: Bucket
        public var byProvider: [String: Bucket]
        public init(today: Bucket, week: Bucket, month: Bucket, byProvider: [String: Bucket]) {
            self.today = today; self.week = week; self.month = month; self.byProvider = byProvider
        }

        private enum CodingKeys: String, CodingKey { case today, week, month, byProvider = "by_provider" }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            today = try c.decode(Bucket.self, forKey: .today)
            week = try c.decode(Bucket.self, forKey: .week)
            month = try c.decode(Bucket.self, forKey: .month)
            byProvider = try c.decodeIfPresent([String: Bucket].self, forKey: .byProvider) ?? [:]
        }
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
        case "answer":
            self = .answer(block: try c.decode(String.self, forKey: .block), keys: try c.decodeIfPresent([String].self, forKey: .keys),
                           text: try c.decodeIfPresent(String.self, forKey: .text))
        case "prefs": self = .prefs(push: try c.decode(Bool.self, forKey: .push))
        case "ask":
            self = .ask(block: try c.decode(String.self, forKey: .block),
                        session: try c.decodeIfPresent(String.self, forKey: .session) ?? "",
                        project: try c.decodeIfPresent(String.self, forKey: .project) ?? "",
                        msg: try c.decodeIfPresent(String.self, forKey: .msg) ?? "",
                        options: try c.decodeIfPresent([String].self, forKey: .options) ?? [])
        case "ask_done": self = .askDone(block: try c.decode(String.self, forKey: .block))
        case "answer_result":
            self = .answerResult(block: try c.decode(String.self, forKey: .block), ok: try c.decode(Bool.self, forKey: .ok),
                                 msg: try c.decodeIfPresent(String.self, forKey: .msg))
        case "cost": self = .cost(usd: try c.decode(Double.self, forKey: .usd), seconds: try c.decode(Double.self, forKey: .seconds))
        case "stats": self = .stats(try Stats(from: decoder))
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
        case let .answer(block, keys, text):
            try c.encode("answer", forKey: .t)
            try c.encode(block, forKey: .block)
            try c.encodeIfPresent(keys, forKey: .keys)
            try c.encodeIfPresent(text, forKey: .text)
        case let .prefs(push):
            try c.encode("prefs", forKey: .t)
            try c.encode(push, forKey: .push)
        case let .ask(block, session, project, msg, options):
            try c.encode("ask", forKey: .t)
            try c.encode(block, forKey: .block)
            try c.encode(session, forKey: .session)
            try c.encode(project, forKey: .project)
            try c.encode(msg, forKey: .msg)
            try c.encode(options, forKey: .options)
        case let .askDone(block):
            try c.encode("ask_done", forKey: .t)
            try c.encode(block, forKey: .block)
        case let .answerResult(block, ok, msg):
            try c.encode("answer_result", forKey: .t)
            try c.encode(block, forKey: .block)
            try c.encode(ok, forKey: .ok)
            try c.encodeIfPresent(msg, forKey: .msg)
        case let .cost(usd, seconds):
            try c.encode("cost", forKey: .t)
            try c.encode(usd, forKey: .usd)
            try c.encode(seconds, forKey: .seconds)
        case let .stats(stats):
            try c.encode("stats", forKey: .t)
            try stats.encode(to: encoder)
        case let .unknown(t):
            try c.encode(t, forKey: .t)
        }
    }
}
