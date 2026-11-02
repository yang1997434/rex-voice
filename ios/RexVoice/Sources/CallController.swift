import AVFAudio
import Foundation
import Observation
import RexVoiceCore
import UIKit
import UserNotifications

/// Ties RelayClient + AudioEngine to what the screen shows.
@MainActor
@Observable
final class CallController {
    enum Link { case unpaired, connecting, macOffline, ready }

    struct Line: Identifiable {
        let id = UUID()
        let fromUser: Bool
        let text: String
        /// Arrival time; the dialog interleaves lines and attachments by it.
        let time = Date()
    }

    private(set) var link = Link.unpaired
    private(set) var inCall = false
    /// Mac's `status.state` during a call: connecting / listening / thinking / speaking.
    private(set) var callState = ""
    private(set) var transcript: [Line] = []

    /// An image or link the Mac sent; kept in memory only, newest last.
    struct Attachment: Identifiable {
        enum Kind { case image(UIImage, name: String), link(URL, title: String) }
        let id = UUID()
        let kind: Kind
        let time = Date()
    }
    /// Last 5, cleared when a call starts.
    private(set) var attachments: [Attachment] = []

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
    /// 5 s cap on playing out the goodbye after `hang_up`.
    @ObservationIgnored private var hangUpTimer: Task<Void, Never>?
    @ObservationIgnored private var pushToken: String?
    /// Images being received: `attach` control first, then its type 4 chunks; dropped after 20 s.
    private struct PendingAttach {
        let name: String
        let size: Int
        let chunks: Int
        var parts: [Int: Data] = [:]
        let timeout: Task<Void, Never>
    }
    @ObservationIgnored private var pending: [Int: PendingAttach] = [:]

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
                enablePush() // no prompt if already answered; also refreshes the token every launch
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
        enablePush()
    }

    /// Asks for alert + sound once, then registers; the token comes back through AppDelegate -> `setPushToken`.
    private func enablePush() {
        Task {
            do {
                _ = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
            } catch {
                notice = "通知权限请求失败：\(error.localizedDescription)"
            }
            UIApplication.shared.registerForRemoteNotifications()
        }
    }

    func setPushToken(_ token: String) {
        pushToken = token
        if let relay { Task { await relay.setPushToken(token) } }
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
        attachments = []
        startMessage = start
        await relay.send(start)
    }

    private func endCall(notice: String?) {
        hangUpTimer?.cancel()
        hangUpTimer = nil
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
        let token = pushToken
        Task {
            if let token { await client.setPushToken(token) }
            await client.start()
        }
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
            // Idle (e.g. back from the background after a hang-up) the link state already says it; only a live
            // call or sample needs telling why it dropped.
            if inCall || previewActive { notice = message } else { link = .connecting }
        case let .audio(pcm):
            if inCall || previewActive { audio.play(pcm) }
        case let .control(m):
            onControl(m)
        case let .attachChunk(id, index, data):
            onChunk(id: id, index: index, data: data)
        }
    }

    private func addAttachment(_ kind: Attachment.Kind) {
        attachments = Array((attachments + [Attachment(kind: kind)]).suffix(5))
    }

    private func onAttach(id: Int, name: String, size: Int, chunks: Int) {
        // 64 KiB frames: 512 chunks is ~32 MiB, far above any screenshot; anything bigger is a bad header.
        guard (1...512).contains(chunks), size > 0 else {
            notice = "图片 \(name) 的分片信息不对，已丢弃"
            return
        }
        pending[id]?.timeout.cancel()
        let timeout = Task { [weak self] in
            try? await Task.sleep(for: .seconds(20))
            guard !Task.isCancelled, let self, self.pending.removeValue(forKey: id) != nil else { return }
            self.notice = "图片 \(name) 没收全，已丢弃"
        }
        pending[id] = PendingAttach(name: name, size: size, chunks: chunks, timeout: timeout)
    }

    private func onChunk(id: Int, index: Int, data: Data) {
        guard var p = pending[id], index < p.chunks else { return } // late, unknown or out of range
        p.parts[index] = data
        guard p.parts.count == p.chunks else {
            pending[id] = p
            return
        }
        pending[id] = nil
        p.timeout.cancel()
        let whole = (0..<p.chunks).reduce(into: Data()) { $0.append(p.parts[$1]!) }
        guard whole.count == p.size, let image = UIImage(data: whole) else {
            notice = "图片 \(p.name) 打不开，已丢弃"
            return
        }
        addAttachment(.image(image, name: p.name))
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
            startMessage = nil // the Mac ended it: a reconnect during the drain must not restart the call
            // Play out the goodbye first; whichever of drain / 5 s comes first ends the call (endCall cancels both).
            hangUpTimer?.cancel()
            hangUpTimer = Task { [weak self] in
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled else { return }
                self?.endCall(notice: text)
            }
            audio.whenDrained { [weak self] in self?.endCall(notice: text) }
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
        case let .attach(id, name, _, size, chunks):
            onAttach(id: id, name: name, size: size, chunks: chunks)
        case let .link(url, title):
            // Opened in Safari: web links only, never app or tel: schemes.
            guard let u = URL(string: url), ["http", "https"].contains(u.scheme?.lowercased()) else {
                notice = "Mac 发来的链接不是网页，已忽略"
                break
            }
            addAttachment(.link(u, title: title.isEmpty ? (u.host() ?? url) : title))
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
            Line(fromUser: false, text: "好，我跟它说先不提交，等你回来再看迁移文件。"),
            Line(fromUser: true, text: "geo 测试的截图发我看看"),
            Line(fromUser: false, text: "截图发过去了，还有 metis 那个 PR 的链接。"),
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
            link = .ready; inCall = true; callState = "thinking"; transcript = Array(talk.prefix(5))
        case .speaking:
            link = .ready; inCall = true; callState = "speaking"
            transcript = Array(talk.prefix(6))
        }
        // `REXVOICE_LINES=<n>` replaces the talk with its first n lines (repeating past the end): long dialogs for screenshots.
        if link == .ready, let n = ProcessInfo.processInfo.environment["REXVOICE_LINES"].flatMap(Int.init), n > 0 {
            transcript = (0..<n).map { talk[$0 % talk.count] }.map { Line(fromUser: $0.fromUser, text: $0.text) }
        }
        // `REXVOICE_ATTACH=1` shows where attachments land: one image + one link in a call.
        if inCall, ProcessInfo.processInfo.environment["REXVOICE_ATTACH"] == "1" {
            let shot = UIGraphicsImageRenderer(size: CGSize(width: 160, height: 100)).image {
                UIColor.systemTeal.setFill(); $0.fill(CGRect(x: 0, y: 0, width: 160, height: 100))
                UIColor.systemOrange.setFill(); $0.fill(CGRect(x: 20, y: 20, width: 60, height: 60))
            }
            attachments = [Attachment(kind: .image(shot, name: "geo-tests.png")),
                           Attachment(kind: .link(URL(string: "https://github.com/yang1997434/metis-va/pull/12")!,
                                                  title: "metis-va PR #12 迁移文件"))]
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

    /// `REXVOICE_SELFTEST=call`: hang-up plays out the queue, image chunks assemble, a missing chunk times out,
    /// links are kept. Real AudioEngine (playback only), no relay. Leaves the attachments on screen for a screenshot.
    func callSelfTest() async {
        var failures: [String] = []
        func check(_ ok: Bool, _ what: String) { if !ok { failures.append(what) } }
        func wait(_ seconds: Double, until done: () -> Bool) async {
            let end = Date().addingTimeInterval(seconds)
            while !done(), Date() < end { try? await Task.sleep(for: .milliseconds(50)) }
        }
        let silence = Data(count: 960) // 20 ms

        // hang_up: the call stays up until the 1 s queue has played, then ends exactly once.
        try? audio.startPlayback()
        inCall = true
        for _ in 0..<50 { handle(.audio(silence)) }
        handle(.control(.hangUp(reason: "user")))
        check(inCall, "hang_up waits for the queue")
        await wait(3) { !inCall }
        check(!inCall && notice == "已挂断" && !audio.isRunning, "hang_up ends after drain")
        notice = nil
        try? await Task.sleep(for: .seconds(5.5))
        check(notice == nil, "5 s fallback did not fire a second time")

        // A dropped link: no notice while idle (just reconnecting), a notice during a call.
        handle(.failed("连接断开：操作无法完成"))
        check(notice == nil && link == .connecting, "failed while idle: no notice")
        inCall = true
        handle(.failed("连接断开：操作无法完成"))
        check(notice == "连接断开：操作无法完成", "failed in a call: notice")
        inCall = false
        notice = nil

        // Image: three chunks out of order -> one attachment.
        let jpeg = UIGraphicsImageRenderer(size: CGSize(width: 160, height: 100)).jpegData(withCompressionQuality: 0.8) {
            UIColor.systemTeal.setFill(); $0.fill(CGRect(x: 0, y: 0, width: 160, height: 100))
            UIColor.systemOrange.setFill(); $0.fill(CGRect(x: 20, y: 20, width: 60, height: 60))
        }
        let third = jpeg.count / 3
        let parts = [jpeg.prefix(third), jpeg.dropFirst(third).prefix(third), jpeg.dropFirst(2 * third)].map { Data($0) }
        handle(.control(.attach(id: 1, name: "shot.jpg", mime: "image/jpeg", size: jpeg.count, chunks: 3)))
        for i in [2, 0, 1] { handle(.attachChunk(id: 1, index: i, data: parts[i])) }
        check(attachments.count == 1 && pending.isEmpty, "chunks assemble into one image")
        if case .image(_, "shot.jpg")? = attachments.first?.kind {} else { check(false, "attachment is the image") }

        // Link: web kept, other schemes refused.
        handle(.control(.link(url: "https://example.com/pull/12", title: "PR #12 修好挂断")))
        handle(.control(.link(url: "tel:123", title: "x")))
        check(attachments.count == 2, "https link kept, tel: refused")

        // Missing chunk: dropped after 20 s with a notice, nothing added.
        handle(.control(.attach(id: 2, name: "half.jpg", mime: "image/jpeg", size: jpeg.count, chunks: 3)))
        handle(.attachChunk(id: 2, index: 0, data: parts[0]))
        check(pending[2] != nil, "partial image pending")
        await wait(21) { pending.isEmpty }
        check(pending.isEmpty && attachments.count == 2 && notice == "图片 half.jpg 没收全，已丢弃", "missing chunk times out")

        print(failures.isEmpty ? "[selftest] PASS" : "[selftest] FAIL: \(failures.joined(separator: "; "))")
    }
}
#endif
