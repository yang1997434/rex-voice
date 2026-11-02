import Foundation

/// Cuts mono 24 kHz Int16 samples into 20 ms frames (480 samples, 960 bytes PCM16 LE); leftovers wait for the next push.
public struct PCMFramer: Sendable {
    public static let frameBytes = 960
    private var pending = Data()

    public init() {}

    public mutating func push(_ samples: some Sequence<Int16>) -> [Data] {
        for s in samples {
            withUnsafeBytes(of: s.littleEndian) { pending.append(contentsOf: $0) }
        }
        var frames: [Data] = []
        while pending.count >= Self.frameBytes {
            frames.append(Data(pending.prefix(Self.frameBytes)))
            pending = Data(pending.dropFirst(Self.frameBytes))
        }
        return frames
    }
}

/// Drops 20 ms frames whose RMS (0...1) is below `threshold`, but keeps sending
/// `hangoverFrames` (300 ms) after the last loud frame so word endings are not cut.
public struct EnergyGate: Sendable {
    public var threshold: Float
    public static let hangoverFrames = 15
    private var hangover = 0

    public init(threshold: Float = 0.01) {
        self.threshold = threshold
    }

    public mutating func shouldSend(frame: Data) -> Bool {
        if Self.rms(frame) >= threshold {
            hangover = Self.hangoverFrames
            return true
        }
        guard hangover > 0 else { return false }
        hangover -= 1
        return true
    }

    /// RMS of PCM16 LE samples, normalized to 0...1.
    static func rms(_ frame: Data) -> Float {
        let n = frame.count / 2
        guard n > 0 else { return 0 }
        var sum: Double = 0
        frame.withUnsafeBytes { raw in
            for i in 0..<n {
                let s = Double(Int16(littleEndian: raw.loadUnaligned(fromByteOffset: i * 2, as: Int16.self)))
                sum += s * s
            }
        }
        return Float((sum / Double(n)).squareRoot() / 32768)
    }
}
