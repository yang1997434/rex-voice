import Foundation

/// JSON body of a type 1 frame, discriminated by `t` (docs/protocol.md section 3).
/// String fields stay plain strings so a new Mac-side value never breaks decoding.
public enum ControlMessage: Codable, Equatable, Sendable {
    // phone -> Mac
    case hello(app: String, device: String, deviceID: String)
    case callStart
    case callEnd
    // Mac -> phone
    /// `detail` is optional: the Mac side currently sends status without it.
    case status(state: String, detail: String?)
    case transcript(who: String, text: String)
    case interrupt
    case hangUp(reason: String)
    case error(msg: String)
    /// Any `t` this build does not know; decoding never throws for it.
    case unknown(String)

    private enum Key: String, CodingKey {
        case t, app, device, deviceID = "device_id", state, detail, who, text, reason, msg
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        let t = try c.decode(String.self, forKey: .t)
        switch t {
        case "hello":
            self = .hello(app: try c.decode(String.self, forKey: .app),
                          device: try c.decode(String.self, forKey: .device),
                          deviceID: try c.decode(String.self, forKey: .deviceID))
        case "call.start": self = .callStart
        case "call.end": self = .callEnd
        case "status":
            self = .status(state: try c.decode(String.self, forKey: .state),
                           detail: try c.decodeIfPresent(String.self, forKey: .detail))
        case "transcript":
            self = .transcript(who: try c.decode(String.self, forKey: .who), text: try c.decode(String.self, forKey: .text))
        case "interrupt": self = .interrupt
        case "hang_up": self = .hangUp(reason: try c.decode(String.self, forKey: .reason))
        case "error": self = .error(msg: try c.decode(String.self, forKey: .msg))
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
        case .callStart: try c.encode("call.start", forKey: .t)
        case .callEnd: try c.encode("call.end", forKey: .t)
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
        case let .unknown(t):
            try c.encode(t, forKey: .t)
        }
    }
}
