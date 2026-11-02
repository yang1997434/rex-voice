import AVFAudio
import Foundation
import Observation
import RexVoiceCore

/// Ties RelayClient + AudioEngine to what the screen shows.
@MainActor
@Observable
final class CallController {
    enum Link { case unpaired, connecting, macOffline, ready }

    struct Line: Identifiable {
        let id = UUID()
        let fromUser: Bool
        let text: String
    }

    private(set) var link = Link.unpaired
    private(set) var inCall = false
    /// Mac's `status.state` during a call: connecting / listening / thinking / speaking.
    private(set) var callState = ""
    private(set) var transcript: [Line] = []
    /// Latest error or hang-up reason; the screen shows it briefly.
    var notice: String?
    var threshold: Double {
        didSet {
            UserDefaults.standard.set(threshold, forKey: Self.thresholdKey)
            audio.threshold = Float(threshold)
        }
    }

    @ObservationIgnored private let audio = AudioEngine()
    @ObservationIgnored private var relay: RelayClient?
    @ObservationIgnored private var eventsTask: Task<Void, Never>?
    @ObservationIgnored private var uplinkTask: Task<Void, Never>?
    @ObservationIgnored private var deviceID = ""

    private static let thresholdKey = "energyThreshold"
    private static let pairingAccount = "pairing"
    private static let deviceAccount = "device_id"

    init() {
        threshold = UserDefaults.standard.object(forKey: Self.thresholdKey) as? Double ?? 0.01
        audio.threshold = Float(threshold)
        audio.onError = { [weak self] in self?.notice = $0 }
        do {
            if let data = try Keychain.get(Self.pairingAccount),
               let text = String(data: data, encoding: .utf8), let url = URL(string: text) {
                try connect(Pairing.parse(url))
            }
        } catch {
            notice = error.localizedDescription
        }
    }

    var statusText: String {
        switch link {
        case .unpaired: return "未配对"
        case .connecting: return "连接中…"
        case .macOffline: return "Mac 不在线"
        case .ready:
            guard inCall else { return "待机" }
            switch callState {
            case "listening": return "通话中 · 在听"
            case "thinking": return "通话中 · 在想"
            case "speaking": return "通话中 · 在说"
            default: return "通话中 · 接通中…"
            }
        }
    }

    var canToggleCall: Bool { inCall || link == .ready }

    /// A scanned QR payload. Throws (and keeps the old pairing) when it is not a valid `rexvoice://pair` URL.
    func pair(with code: String) throws {
        guard let url = URL(string: code) else { throw PairingError.badScheme }
        let p = try Pairing.parse(url)
        try Keychain.set(Data(code.utf8), account: Self.pairingAccount, accessible: kSecAttrAccessibleWhenUnlockedThisDeviceOnly)
        endCall(notice: nil)
        transcript = []
        try connect(p)
    }

    func toggleCall() async {
        if inCall {
            await relay?.send(.callEnd)
            endCall(notice: nil)
            return
        }
        guard link == .ready, let relay else { return }
        guard await AVAudioApplication.requestRecordPermission() else {
            notice = "没有麦克风权限：到 设置 > Rex Voice 打开"
            return
        }
        let (frames, sink) = AsyncStream.makeStream(of: Data.self, bufferingPolicy: .bufferingNewest(50))
        do {
            try audio.start { sink.yield($0) }
        } catch {
            notice = "音频启动失败：\(error.localizedDescription)"
            return
        }
        // One consumer, in order: seq must reach the Mac in the order frames were sealed.
        uplinkTask = Task.detached {
            for await f in frames { await relay.sendAudio(f) }
        }
        inCall = true
        callState = "connecting"
        transcript = []
        await relay.send(.callStart)
    }

    private func endCall(notice: String?) {
        uplinkTask?.cancel()
        uplinkTask = nil
        audio.stop()
        inCall = false
        callState = ""
        if let notice { self.notice = notice }
    }

    private func connect(_ pairing: Pairing) throws {
        if deviceID.isEmpty { deviceID = try Self.loadDeviceID() }
        eventsTask?.cancel()
        if let old = relay { Task { await old.stop() } }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        let client = RelayClient(pairing: pairing, userAgent: "RexVoice-iOS/\(version)",
                                 hello: .hello(app: version, device: Self.machine(), deviceID: deviceID))
        relay = client
        link = .connecting
        eventsTask = Task { [weak self] in
            for await e in client.events { self?.handle(e) }
        }
        Task { await client.start() }
    }

    private func handle(_ event: RelayClient.Event) {
        switch event {
        case .connecting:
            link = .connecting
        case .macOffline:
            link = .macOffline
        case .ready:
            link = .ready
            // The Mac keeps a call across a phone reconnect; call.start is idempotent there.
            if inCall, let relay { Task { await relay.send(.callStart) } }
        case let .failed(message):
            notice = message
        case let .audio(pcm):
            if inCall { audio.play(pcm) }
        case let .control(m):
            onControl(m)
        }
    }

    private func onControl(_ m: ControlMessage) {
        switch m {
        case let .status(state, _):
            if inCall, state != "ended" { callState = state }
        case let .transcript(who, text):
            transcript.append(Line(fromUser: who == "user", text: text))
        case .interrupt:
            audio.flushPlayback()
        case let .hangUp(reason):
            let text = switch reason {
            case "user": "已挂断"
            case "idle": "太久没说话，已自动挂断"
            case "error": "通话出错，已挂断"
            default: "已挂断（\(reason)）"
            }
            endCall(notice: text)
        case let .error(msg):
            notice = "Mac：\(msg)"
        case .hello, .callStart, .callEnd, .unknown:
            break
        }
    }

    private static func loadDeviceID() throws -> String {
        if let d = try Keychain.get(deviceAccount), let s = String(data: d, encoding: .utf8) { return s }
        let id = UUID().uuidString
        try Keychain.set(Data(id.utf8), account: deviceAccount, accessible: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)
        return id
    }

    /// Model identifier such as "iPhone17,1".
    private static func machine() -> String {
        var u = utsname()
        uname(&u)
        return withUnsafeBytes(of: &u.machine) { String(decoding: $0.prefix { $0 != 0 }, as: UTF8.self) }
    }
}

#if DEBUG
extension CallController {
    /// Design previews only (`REXVOICE_PREVIEW`): fake link / call state and transcript, no relay or audio.
    func applyPreview(_ screen: PreviewScreen) {
        let talk = [
            Line(fromUser: true, text: "看一下 geo 那个窗格跑到哪了"),
            Line(fromUser: false, text: "geo 窗格里 Claude 在跑全量测试，412 条过了 398 条，还没有失败。"),
            Line(fromUser: true, text: "好。metis 那边呢？"),
            Line(fromUser: false, text: "metis-va 窗格停在等你确认：它问要不要把迁移文件一起提交。"),
            Line(fromUser: true, text: "让它先别提交，等我回去看"),
        ]
        notice = nil
        switch screen {
        case .unpaired:
            link = .unpaired; inCall = false; transcript = []
        case .connecting:
            link = .connecting; inCall = false; transcript = []
        case .offline:
            link = .macOffline; inCall = false; transcript = []
            notice = "Mac 断开了，正在等它重新上线"
        case .idle, .settings, .pair:
            link = .ready; inCall = false; transcript = Array(talk.prefix(4))
            if screen == .idle { notice = "太久没说话，已自动挂断" }
        case .listening:
            link = .ready; inCall = true; callState = "listening"; transcript = Array(talk.prefix(4))
        case .thinking:
            link = .ready; inCall = true; callState = "thinking"; transcript = talk
        case .speaking:
            link = .ready; inCall = true; callState = "speaking"
            transcript = talk + [Line(fromUser: false, text: "好，我跟它说先不提交，等你回来再看迁移文件。")]
        }
    }
}
#endif
