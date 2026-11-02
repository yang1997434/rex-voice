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

    enum PreviewState: Equatable { case idle, connecting, playing, failed(String) }
    /// Voice sample from the settings page: the Mac runs a short session, audio comes as type 0 frames.
    /// `failed` goes back to `idle` by itself after 3 s.
    private(set) var previewState = PreviewState.idle
    /// Latest error or hang-up reason; the screen shows it briefly.
    var notice: String?
    var threshold: Double {
        didSet {
            UserDefaults.standard.set(threshold, forKey: Self.thresholdKey)
            audio.threshold = Float(threshold)
        }
    }

    /// Relay host of the current pairing (shown on screen; the Mac has no name in the pairing).
    private(set) var relayHost: String?
    /// When this phone scanned the current pairing; nil for pairings made before this was recorded.
    private(set) var pairedAt: Date?
    /// Mic level 0...1 while a call runs (see `AudioEngine.level`).
    var level: Float { audio.level }

    @ObservationIgnored private let audio = AudioEngine()
    @ObservationIgnored private let settings: AppSettings
    /// The `call.start` of the running call, resent as-is after a reconnect. Holds the API key only for the call.
    @ObservationIgnored private var startMessage: ControlMessage?
    @ObservationIgnored private var relay: RelayClient?
    @ObservationIgnored private var eventsTask: Task<Void, Never>?
    @ObservationIgnored private var uplinkTask: Task<Void, Never>?
    @ObservationIgnored private var deviceID = ""
    /// 25 s fallback while a sample runs, then the 3 s clear of `failed`.
    @ObservationIgnored private var previewTimer: Task<Void, Never>?

    private static let thresholdKey = "energyThreshold"
    private static let pairingAccount = "pairing"
    private static let deviceAccount = "device_id"
    private static let pairedAtKey = "pairedAt"

    init(settings: AppSettings) {
        self.settings = settings
        pairedAt = UserDefaults.standard.object(forKey: Self.pairedAtKey) as? Date
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

    var canToggleCall: Bool { inCall || link == .ready }
    /// Same Mac-online check as「开始通话」; the voice cannot change mid-call, so no sample then.
    var canPreview: Bool { !inCall && link == .ready }
    private var previewActive: Bool { previewState == .connecting || previewState == .playing }

    /// Plays a sample of the picked voice. Pressing again while one runs sends a fresh request (the Mac restarts it).
    func previewVoice() async {
        guard canPreview, let relay else { return }
        let msg: ControlMessage
        do {
            msg = try settings.voicePreview()
        } catch {
            endPreview(.failed(error.localizedDescription))
            return
        }
        await runPreview { await relay.send(msg) }
    }

    /// Output-only audio up, state to connecting, 25 s fallback armed, then `send`.
    private func runPreview(send: () async -> Bool) async {
        if audio.isRunning {
            audio.flushPlayback() // the previous sample's leftover audio
        } else {
            do {
                try audio.startPlayback()
            } catch {
                endPreview(.failed("音频启动失败：\(error.localizedDescription)"))
                return
            }
        }
        previewState = .connecting
        previewTimer?.cancel()
        previewTimer = Task { [weak self] in
            try? await Task.sleep(for: .seconds(25))
            guard !Task.isCancelled else { return }
            self?.endPreview(.failed("超时"))
        }
        // A call started while sending already cleared the sample; nothing to report then.
        if !(await send()), previewActive { endPreview(.failed("Mac 没收到")) }
    }

    private func endPreview(_ next: PreviewState) {
        previewTimer?.cancel()
        previewTimer = nil
        if !inCall { audio.stop() }
        previewState = next
        guard case .failed = next else { return }
        previewTimer = Task { [weak self] in
            try? await Task.sleep(for: .seconds(3))
            guard !Task.isCancelled, let self, case .failed = self.previewState else { return }
            self.previewState = .idle
        }
    }

    /// A scanned QR payload. Throws (and keeps the old pairing) when it is not a valid `rexvoice://pair` URL.
    func pair(with code: String) throws {
        guard let url = URL(string: code) else { throw PairingError.badScheme }
        let p = try Pairing.parse(url)
        try Keychain.set(Data(code.utf8), account: Self.pairingAccount, accessible: kSecAttrAccessibleWhenUnlockedThisDeviceOnly)
        pairedAt = Date()
        UserDefaults.standard.set(pairedAt, forKey: Self.pairedAtKey)
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
        // The Mac cancels the sample on call.start (and answers `preview error 已取消`, ignored below).
        if previewState != .idle { endPreview(.idle) }
        let start: ControlMessage
        do {
            start = try settings.callStart()
        } catch {
            notice = error.localizedDescription
            return
        }
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
        startMessage = start
        await relay.send(start)
    }

    private func endCall(notice: String?) {
        uplinkTask?.cancel()
        uplinkTask = nil
        audio.stop()
        inCall = false
        callState = ""
        startMessage = nil
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
        relayHost = pairing.relay.host()
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
            if previewActive { endPreview(.failed("Mac 不在线")) }
        case .ready:
            link = .ready
            // The Mac keeps a call across a phone reconnect; call.start is idempotent there.
            if inCall, let relay, let startMessage { Task { await relay.send(startMessage) } }
        case let .failed(message):
            notice = message
        case let .audio(pcm):
            if inCall || previewActive { audio.play(pcm) }
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
        case let .preview(state, msg):
            guard previewActive else { break } // late messages after done / timeout / call.start
            switch state {
            case "connecting": previewState = .connecting
            case "playing": previewState = .playing
            case "done":
                previewTimer?.cancel()
                audio.whenDrained { [weak self] in
                    guard let self, self.previewActive else { return }
                    self.endPreview(.idle)
                }
            // The Mac cancelled the old sample for a newer request; the newer one reports on its own.
            case "error" where msg == "已取消": break
            case "error": endPreview(.failed(msg ?? "试听失败"))
            default: break
            }
        case .hello, .callStart, .callEnd, .voicePreview, .unknown:
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

        if screen != .unpaired {
            relayHost = "r.maruiao.top"
            pairedAt = Calendar.current.date(from: DateComponents(year: 2026, month: 11, day: 1, hour: 21, minute: 30))
        }
        audio.setPreviewLevel(screen == .listening ? 0.7 : 0)
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
        // `REXVOICE_PREVIEW_STATE=connecting|playing|failed` freezes the voice-sample button; `incall` greys it.
        switch ProcessInfo.processInfo.environment["REXVOICE_PREVIEW_STATE"] {
        case "connecting": previewState = .connecting
        case "playing": previewState = .playing
        case "failed": previewState = .failed("Mac：没有这个音色")
        case "incall": inCall = true; callState = "listening"
        default: break
        }
    }

    /// `REXVOICE_SELFTEST=preview`: drives the voice-sample state machine with fake Mac messages and the real
    /// AudioEngine (playback only), prints `[selftest] PASS` or the failed checks. No relay involved.
    func previewSelfTest() async {
        var failures: [String] = []
        func check(_ ok: Bool, _ what: String) { if !ok { failures.append(what) } }
        // 1 kHz sine, 20 ms PCM16 LE 24 kHz frames, 1 s in all.
        let frame = Data((0..<480).flatMap { i -> [UInt8] in
            let s = Int16(sin(2 * Double.pi * 1000 * Double(i) / 24_000) * 0.3 * 32767)
            return withUnsafeBytes(of: s.littleEndian, Array.init)
        })
        func waitIdle(_ seconds: Double) async {
            let end = Date().addingTimeInterval(seconds)
            while previewState != .idle, Date() < end { try? await Task.sleep(for: .milliseconds(50)) }
        }

        check(previewState == .idle, "starts idle")
        await runPreview { true }
        check(previewState == .connecting && audio.isRunning, "connecting after send, engine up")
        handle(.control(.preview(state: "connecting", msg: nil)))
        check(previewState == .connecting, "preview connecting")
        handle(.control(.preview(state: "playing", msg: nil)))
        check(previewState == .playing, "preview playing")
        for _ in 0..<50 { handle(.audio(frame)) }
        handle(.control(.preview(state: "done", msg: nil)))
        check(previewState == .playing, "still playing until the queue drains")
        await waitIdle(3)
        check(previewState == .idle && !audio.isRunning, "idle and engine off after drain")

        await runPreview { true }
        handle(.control(.preview(state: "error", msg: "已取消")))
        check(previewState == .connecting, "已取消 ignored")
        handle(.control(.preview(state: "error", msg: "没有这个音色")))
        check(previewState == .failed("没有这个音色") && !audio.isRunning, "error -> failed, engine off")
        await waitIdle(3.5)
        check(previewState == .idle, "failed clears after 3 s")

        await runPreview { false }
        check(previewState == .failed("Mac 没收到") && !audio.isRunning, "send failure -> failed")

        print(failures.isEmpty ? "[selftest] PASS" : "[selftest] FAIL: \(failures.joined(separator: "; "))")
    }
}
#endif
