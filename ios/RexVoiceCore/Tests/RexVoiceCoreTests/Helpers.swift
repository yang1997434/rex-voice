import CryptoKit
import Foundation

extension Data {
    init(hex: String) {
        precondition(hex.count % 2 == 0, "odd hex length")
        var bytes: [UInt8] = []
        var i = hex.startIndex
        while i < hex.endIndex {
            let j = hex.index(i, offsetBy: 2)
            bytes.append(UInt8(hex[i..<j], radix: 16)!)
            i = j
        }
        self.init(bytes)
    }

    var hex: String { map { String(format: "%02x", $0) }.joined() }
}

/// 0x10...0x1f and 0x20...0x2f: the session salts used by docs/protocol.md vectors.
let phoneSalt = Data((0x10...0x1f).map { UInt8($0) })
let macSalt = Data((0x20...0x2f).map { UInt8($0) })
let testKey = SymmetricKey(data: Data((0...31).map { UInt8($0) }))

let protocolDoc = try! String(
    contentsOf: URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("docs/protocol.md"),
    encoding: .utf8)
