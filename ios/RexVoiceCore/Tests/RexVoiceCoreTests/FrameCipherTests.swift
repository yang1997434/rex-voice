import CryptoKit
import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct FrameTests {
    @Test func saltFrame() {
        let frame = SaltFrame.make(macSalt)
        #expect(frame.hex == "0200000000202122232425262728292a2b2c2d2e2f") // from docs/protocol.md
        #expect(SaltFrame.parse(frame) == macSalt)
        #expect(SaltFrame.parse(frame.dropLast()) == nil)
        var badSeq = frame
        badSeq[1] = 1
        #expect(SaltFrame.parse(badSeq) == nil)
        #expect(SaltFrame.parse(KeepaliveFrame.data + Data(count: 16)) == nil)
    }

    @Test func keepalive() {
        #expect(KeepaliveFrame.data.hex == "0300000000")
    }
}

@Suite struct FrameCipherTests {
    func pair(_ dir: FrameCipher.Direction, key: SymmetricKey = testKey) -> (FrameCipher, FrameCipher) {
        (FrameCipher(key: key, direction: dir, phoneSalt: phoneSalt, macSalt: macSalt),
         FrameCipher(key: key, direction: dir, phoneSalt: phoneSalt, macSalt: macSalt))
    }

    @Test func roundTrip() throws {
        let (tx, rx) = pair(.phoneToMac)
        for i in 1...3 {
            let plain = Data("frame \(i)".utf8)
            let frame = try tx.seal(type: .audio, plain)
            #expect(frame.count == 5 + plain.count + 16) // no nonce on the wire
            let got = try rx.open(frame)
            #expect(got.type == .audio && got.seq == UInt32(i) && got.plain == plain)
        }
    }

    @Test func directionsUseDifferentKeys() throws {
        let p2m = FrameCipher(key: testKey, direction: .phoneToMac, phoneSalt: phoneSalt, macSalt: macSalt)
        let m2p = FrameCipher(key: testKey, direction: .macToPhone, phoneSalt: phoneSalt, macSalt: macSalt)
        let frame = try p2m.seal(type: .control, Data("x".utf8))
        #expect(throws: FrameCipherError.badFrame) { try m2p.open(frame) }
    }

    @Test func saltsChangeKey() throws {
        let tx = FrameCipher(key: testKey, direction: .phoneToMac, phoneSalt: phoneSalt, macSalt: macSalt)
        let rx = FrameCipher(key: testKey, direction: .phoneToMac, phoneSalt: phoneSalt, macSalt: Data(count: 16))
        #expect(throws: FrameCipherError.badFrame) { try rx.open(try tx.seal(type: .audio, Data([1]))) }
    }

    @Test func tamperedTypeFails() throws {
        let (tx, rx) = pair(.phoneToMac)
        var frame = try tx.seal(type: .control, Data("{}".utf8))
        frame[0] = FrameType.audio.rawValue
        #expect(throws: FrameCipherError.badFrame) { try rx.open(frame) }
    }

    @Test func tamperedBodyAndShortFail() throws {
        let (tx, rx) = pair(.phoneToMac)
        var frame = try tx.seal(type: .audio, Data([1, 2, 3]))
        frame[frame.count - 1] ^= 1
        #expect(throws: FrameCipherError.badFrame) { try rx.open(frame) }
        #expect(throws: FrameCipherError.badFrame) { try rx.open(Data([0, 0, 0, 0, 1])) }
        #expect(throws: FrameCipherError.badFrame) { try rx.open(Data([9, 0, 0, 0, 1]) + Data(count: 16)) }
    }

    @Test func replayRejected() throws {
        let (tx, rx) = pair(.macToPhone)
        let f1 = try tx.seal(type: .audio, Data([1]))
        let f2 = try tx.seal(type: .audio, Data([2]))
        _ = try rx.open(f2)
        #expect(throws: FrameCipherError.replay) { try rx.open(f2) } // duplicate
        #expect(throws: FrameCipherError.replay) { try rx.open(f1) } // went back
    }

    @Test func seqAdvancesOnlyOnSuccess() throws {
        let (tx, rx) = pair(.phoneToMac)
        let f1 = try tx.seal(type: .audio, Data([1]))
        var forged = try tx.seal(type: .audio, Data([2])) // seq 2
        forged[forged.count - 1] ^= 1
        #expect(throws: FrameCipherError.badFrame) { try rx.open(forged) }
        #expect(try rx.open(f1).seq == 1) // the forged seq 2 did not move the window
    }

    @Test func sealStopsAtMaxSeq() throws {
        let (tx, rx) = pair(.phoneToMac)
        tx.seq = .max - 1
        let last = try tx.seal(type: .audio, Data([1]))
        #expect(try rx.open(last).seq == .max)
        #expect(throws: FrameCipherError.seqExhausted) { try tx.seal(type: .audio, Data([2])) }
    }

    struct Vector: Sendable {
        let key: SymmetricKey
        let dir: FrameCipher.Direction
        let seq: UInt32
        let type: FrameType
        let plain: Data
        let frame: String
    }

    // Copied verbatim from the docs/protocol.md section 3 table; the test also checks the frame hex is still in the doc.
    static let vectors = [
        Vector(key: testKey, dir: .phoneToMac, seq: 1, type: .control,
               plain: Data(#"{"t":"hello","app":"1.0","device":"iPhone"}"#.utf8),
               frame: "0100000001cf157aea72a115abc5c127fdb119375ca71ca651a2ef7a09586971c4b055cb5ab9d484c0feac3a2cb3184af2492fcf576c2b891fb06fb968fa8e32"),
        Vector(key: testKey, dir: .macToPhone, seq: 2, type: .control,
               plain: Data(#"{"t":"ping"}"#.utf8),
               frame: "0100000002bb7fdebff7ddfc8ac040de173242f85d62a57994c8c9ed473578f5ff"),
        Vector(key: SymmetricKey(data: Data(repeating: 0x42, count: 32)), dir: .phoneToMac, seq: 0x0102_0304, type: .audio,
               plain: Data(hex: "0001020304050607"),
               frame: "000102030427cab922deb03fa6d2de530290b8ced22f46ef267361583b"),
    ]

    @Test(arguments: 0..<3) func interopVector(i: Int) throws {
        let v = Self.vectors[i]
        #expect(protocolDoc.contains(v.frame))
        let (tx, rx) = pair(v.dir, key: v.key)
        tx.seq = v.seq - 1
        #expect(try tx.seal(type: v.type, v.plain).hex == v.frame)
        let got = try rx.open(Data(hex: v.frame))
        #expect(got.type == v.type && got.seq == v.seq && got.plain == v.plain)
    }
}
