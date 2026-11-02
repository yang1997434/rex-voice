import Foundation
import Testing
@testable import RexVoiceCore

@Suite struct HandshakeTests {
    @Test func saltExchangeDerivesKeysOnce() throws {
        var hs = Handshake(key: testKey)
        let first = hs.connected()
        #expect(SaltFrame.parse(first) == hs.phoneSalt)
        #expect(!hs.ready)
        // present:true twice before the Mac salt: same phone salt resent, not a new one
        let again1 = hs.peer(present: true), again2 = hs.peer(present: true)
        #expect(again1 == first && again2 == first)
        let derived = hs.mac(salt: macSalt)
        #expect(derived && hs.ready)
        // repeated Mac salt is ignored (seq must not restart under the same keys)
        let repeated = hs.mac(salt: macSalt)
        #expect(!repeated)
        let mac = FrameCipher(key: testKey, direction: .macToPhone, phoneSalt: hs.phoneSalt, macSalt: macSalt)
        let opened = try hs.rx!.open(try mac.seal(type: .control, Data("hi".utf8)))
        #expect(opened.plain == Data("hi".utf8))
    }

    @Test func macGoingAwayAndBackRotatesSalt() {
        var hs = Handshake(key: testKey)
        _ = hs.connected()
        _ = hs.mac(salt: macSalt)
        let old = hs.phoneSalt
        let gone = hs.peer(present: false)
        #expect(gone == nil && !hs.ready)
        // Mac back after a completed handshake: new phone salt, keys gone until the next Mac salt
        let frame = hs.peer(present: true)
        #expect(frame != nil && SaltFrame.parse(frame!) != old)
        #expect(!hs.ready)
        let derived = hs.mac(salt: macSalt)
        #expect(derived && hs.ready)
    }
}
