import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct AudioTests {
    /// #expect captures its operands immutably, so the mutating call goes through here.
    func send(_ g: inout EnergyGate, _ frame: Data) -> Bool { g.shouldSend(frame: frame) }

    @Test func framerKeepsRemainderAcrossPushes() {
        var f = PCMFramer()
        #expect(f.push((0..<300).map { Int16($0) }).isEmpty)
        let out = f.push((300..<1000).map { Int16($0) }) // 1000 samples total -> 2 frames + 40 left
        #expect(out.count == 2 && out.allSatisfy { $0.count == 960 })
        #expect(out[0].prefix(4) == Data([0, 0, 1, 0])) // little-endian 0, 1
        #expect(out[1].prefix(2) == Data([0xe0, 0x01])) // sample 480
        let next = f.push((1000..<1440).map { Int16($0) })
        #expect(next.count == 1 && next[0].prefix(2) == Data([0xc0, 0x03])) // sample 960 continues
    }

    static func frame(_ amplitude: Int16) -> Data {
        var f = PCMFramer()
        return f.push((0..<480).map { $0 % 2 == 0 ? amplitude : -amplitude })[0]
    }

    @Test func gateDropsSilenceAndSendsVoice() {
        var g = EnergyGate()
        #expect(!send(&g, Self.frame(0)))
        #expect(!send(&g, Self.frame(100))) // RMS ~0.003 < 0.01
        #expect(send(&g, Self.frame(3000))) // RMS ~0.09
        #expect(abs(EnergyGate.rms(Self.frame(16384)) - 0.5) < 0.001)
    }

    @Test func gateHangoverIs15Frames() {
        var g = EnergyGate()
        #expect(send(&g, Self.frame(3000)))
        for _ in 0..<15 { #expect(send(&g, Self.frame(0))) }
        #expect(!send(&g, Self.frame(0)))
    }

    @Test func gateThresholdIsAdjustable() {
        var g = EnergyGate(threshold: 0.2)
        #expect(!send(&g, Self.frame(3000)))
        g.threshold = 0.001
        #expect(send(&g, Self.frame(100)))
    }
}
