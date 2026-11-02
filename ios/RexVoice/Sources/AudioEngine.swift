import AVFAudio
import Foundation
import Observation
import QuartzCore
import RexVoiceCore

/// Microphone -> 24 kHz mono PCM16 20 ms frames (gated), and playback of the Mac's PCM16 24 kHz.
/// Recovers from interruptions (calls, Siri), route changes (headphones) and engine resets.
@MainActor
@Observable
final class AudioEngine {
    enum Failure: LocalizedError {
        case noInput, noConverter
        var errorDescription: String? {
            switch self {
            case .noInput: "没有可用的麦克风"
            case .noConverter: "麦克风格式无法转换"
            }
        }
    }

    /// Called on the main actor with a short message when audio fails after start.
    @ObservationIgnored var onError: ((String) -> Void)?
    @ObservationIgnored var threshold: Float = 0.01 {
        didSet { uplink?.setThreshold(threshold) }
    }
    /// Mic input level before the gate, 0...1 on a log scale (`Self.normalized`), updated every 50 ms while running.
    private(set) var level: Float = 0

    /// RMS (0...1) to the meter scale: -60 dBFS..0 dBFS -> 0...1, so quiet speech still moves the bar.
    nonisolated static func normalized(_ rms: Float) -> Float {
        guard rms > 0 else { return 0 }
        return min(1, max(0, (20 * log10(rms) + 60) / 60))
    }

    /// A fresh engine per start: a playback-only run must not inherit the call's voice-processing input unit.
    @ObservationIgnored private var engine = AVAudioEngine()
    @ObservationIgnored private var player = AVAudioPlayerNode()
    /// The mixer resamples this to the hardware output format.
    @ObservationIgnored private let playFormat = AVAudioFormat(standardFormatWithSampleRate: 24_000, channels: 1)!
    /// nil while running playback-only (voice sample): the input node is never touched then.
    @ObservationIgnored private var uplink: Uplink?
    @ObservationIgnored private var observers: [NSObjectProtocol] = []
    @ObservationIgnored private var running = false
    /// Buffers scheduled but not yet played back; `generation` drops completions from before a flush / stop.
    @ObservationIgnored private var queued = 0
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var onDrained: (() -> Void)?

    /// Whether the engine is up (call or voice sample).
    var isRunning: Bool { running }

    func start(onFrame: @escaping @Sendable (Data) -> Void) throws {
        guard !running else { return }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat,
                                options: [.allowBluetoothHFP, .allowBluetoothA2DP, .defaultToSpeaker])
        let up = Uplink(threshold: threshold, onFrame: onFrame, onLevel: { [weak self] rms in
            Task { @MainActor in
                guard let self, self.running else { return }
                self.level = Self.normalized(rms)
            }
        }) { [weak self] message in
            Task { @MainActor in self?.onError?(message) }
        }
        try begin(uplink: up)
    }

    /// Output only, no microphone: `.playback` goes to the speaker (or headphones), not the earpiece.
    func startPlayback() throws {
        guard !running else { return }
        try AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        try begin(uplink: nil)
    }

    private func begin(uplink up: Uplink?) throws {
        let session = AVAudioSession.sharedInstance()
        try session.setActive(true)
        engine = AVAudioEngine()
        player = AVAudioPlayerNode()
        engine.attach(player)
        uplink = up
        do {
            try startGraph()
        } catch {
            uplink = nil
            try? session.setActive(false, options: .notifyOthersOnDeactivation)
            throw error
        }
        running = true
        observe()
    }

    func stop() {
        guard running else { return }
        running = false
        observers.forEach { NotificationCenter.default.removeObserver($0) }
        observers = []
        if uplink != nil { engine.inputNode.removeTap(onBus: 0) }
        player.stop()
        engine.stop()
        uplink = nil
        level = 0
        generation += 1
        queued = 0
        onDrained = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    #if DEBUG
    /// Screenshots only: a fixed level for the listening halo without a microphone.
    func setPreviewLevel(_ v: Float) { level = v }
    #endif

    /// Queues one PCM16 LE 24 kHz mono chunk from the Mac.
    func play(_ pcm: Data) {
        guard running, engine.isRunning else { return }
        let n = pcm.count / 2
        guard n > 0, let buf = AVAudioPCMBuffer(pcmFormat: playFormat, frameCapacity: AVAudioFrameCount(n)) else { return }
        buf.frameLength = AVAudioFrameCount(n)
        let out = buf.floatChannelData![0]
        pcm.withUnsafeBytes { raw in
            for i in 0..<n {
                out[i] = Float(Int16(littleEndian: raw.loadUnaligned(fromByteOffset: i * 2, as: Int16.self))) / 32768
            }
        }
        queued += 1
        let gen = generation
        player.scheduleBuffer(buf, completionCallbackType: .dataPlayedBack) { [weak self] _ in
            Task { @MainActor in self?.played(gen) }
        }
    }

    /// Calls `f` once everything queued so far has played (right away when nothing is queued).
    /// Dropped by `stop()` and `flushPlayback()`.
    func whenDrained(_ f: @escaping () -> Void) {
        if queued == 0 { f() } else { onDrained = f }
    }

    private func played(_ gen: Int) {
        guard gen == generation, queued > 0 else { return }
        queued -= 1
        guard queued == 0, let f = onDrained else { return }
        onDrained = nil
        f()
    }

    /// `interrupt` from the Mac: drop everything queued.
    func flushPlayback() {
        guard running else { return }
        generation += 1
        queued = 0
        player.stop()
        if engine.isRunning { player.play() }
        onDrained = nil
    }

    private func startGraph() throws {
        if uplink != nil {
            let input = engine.inputNode
            // Echo cancellation: without it the model hears itself through the speaker and interrupts itself.
            if !input.isVoiceProcessingEnabled { try input.setVoiceProcessingEnabled(true) }
            let format = input.outputFormat(forBus: 0)
            guard format.sampleRate > 0, format.channelCount > 0 else { throw Failure.noInput }
            try uplink?.install(on: input, format: format)
        }
        engine.connect(player, to: engine.mainMixerNode, format: playFormat)
        engine.prepare()
        try engine.start()
        player.play()
    }

    /// Rebuilds the graph after the system stopped or reconfigured the engine.
    private func restart() {
        guard running else { return }
        if uplink != nil { engine.inputNode.removeTap(onBus: 0) }
        player.stop()
        engine.stop()
        do {
            try AVAudioSession.sharedInstance().setActive(true)
            try startGraph()
        } catch {
            onError?("音频恢复失败：\(error.localizedDescription)")
        }
    }

    private func observe() {
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification,
                                            object: AVAudioSession.sharedInstance(), queue: .main) { [weak self] n in
            let raw = n.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt
            MainActor.assumeIsolated {
                guard let self, let raw, let type = AVAudioSession.InterruptionType(rawValue: raw) else { return }
                switch type {
                case .began: self.onError?("通话被系统打断（来电或 Siri）")
                case .ended: self.restart()
                @unknown default: break
                }
            }
        })
        observers.append(center.addObserver(forName: AVAudioSession.routeChangeNotification,
                                            object: AVAudioSession.sharedInstance(), queue: .main) { [weak self] n in
            let raw = n.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
            MainActor.assumeIsolated {
                guard let self, let raw, let reason = AVAudioSession.RouteChangeReason(rawValue: raw) else { return }
                if (reason == .newDeviceAvailable || reason == .oldDeviceUnavailable) && !self.engine.isRunning {
                    self.restart()
                }
            }
        })
        observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange,
                                            object: engine, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.restart() }
        })
        observers.append(center.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification,
                                            object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.restart() }
        })
    }
}

/// Runs on the audio tap thread: convert, cut into 20 ms frames, gate, hand out.
private final class Uplink: @unchecked Sendable {
    private let lock = NSLock()
    private var gate: EnergyGate // guarded by lock
    private var framer = PCMFramer() // guarded by lock
    private var converter: AVAudioConverter? // guarded by lock
    private var reported = false // guarded by lock
    private var peak: Float = 0 // guarded by lock; loudest buffer RMS since the last level report
    private var lastReport: CFTimeInterval = 0 // guarded by lock
    private let onFrame: @Sendable (Data) -> Void
    private let onLevel: @Sendable (Float) -> Void
    private let onError: @Sendable (String) -> Void
    private let outFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 24_000, channels: 1, interleaved: true)!

    init(threshold: Float, onFrame: @escaping @Sendable (Data) -> Void, onLevel: @escaping @Sendable (Float) -> Void,
         onError: @escaping @Sendable (String) -> Void) {
        gate = EnergyGate(threshold: threshold)
        self.onFrame = onFrame
        self.onLevel = onLevel
        self.onError = onError
    }

    func setThreshold(_ t: Float) {
        lock.withLock { gate.threshold = t }
    }

    func install(on node: AVAudioInputNode, format: AVAudioFormat) throws {
        guard let c = AVAudioConverter(from: format, to: outFormat) else { throw AudioEngine.Failure.noConverter }
        c.downmix = true
        lock.withLock {
            converter = c
            framer = PCMFramer()
        }
        node.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buf, _ in
            self?.process(buf)
        }
    }

    private func process(_ input: AVAudioPCMBuffer) {
        let (frames, failure, level) = lock.withLock { () -> ([Data], String?, Float?) in
            guard let converter else { return ([], nil, nil) }
            let capacity = AVAudioFrameCount(Double(input.frameLength) * outFormat.sampleRate / input.format.sampleRate) + 64
            guard let out = AVAudioPCMBuffer(pcmFormat: outFormat, frameCapacity: capacity) else { return ([], nil, nil) }
            var fed = false
            var error: NSError?
            let status = converter.convert(to: out, error: &error) { _, inputStatus in
                if fed {
                    inputStatus.pointee = .noDataNow
                    return nil
                }
                fed = true
                inputStatus.pointee = .haveData
                return input
            }
            guard status != .error else {
                defer { reported = true }
                return ([], reported ? nil : "麦克风转换失败：\(error?.localizedDescription ?? "未知错误")", nil)
            }
            let samples = UnsafeBufferPointer(start: out.int16ChannelData![0], count: Int(out.frameLength))
            peak = max(peak, Self.rms(samples))
            // Throttled to 20 Hz so the main actor (and any meter view) is not woken per buffer.
            var report: Float?
            let now = CACurrentMediaTime()
            if now - lastReport >= 0.05 {
                report = peak
                peak = 0
                lastReport = now
            }
            return (framer.push(samples).filter { gate.shouldSend(frame: $0) }, nil, report)
        }
        if let failure { onError(failure) }
        if let level { onLevel(level) }
        frames.forEach(onFrame)
    }

    private static func rms(_ samples: UnsafeBufferPointer<Int16>) -> Float {
        guard !samples.isEmpty else { return 0 }
        var sum: Double = 0
        for s in samples { sum += Double(s) * Double(s) }
        return Float((sum / Double(samples.count)).squareRoot() / 32768)
    }
}
