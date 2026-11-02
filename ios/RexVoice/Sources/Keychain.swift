import Foundation
import Security

/// Thin wrapper over generic-password items for this app; items never sync to iCloud.
enum Keychain {
    private static let service = "com.maruiao.rexvoice"

    struct Failure: LocalizedError {
        let status: OSStatus
        var errorDescription: String? {
            "钥匙串出错：\(SecCopyErrorMessageString(status, nil) as String? ?? "OSStatus \(status)")"
        }
    }

    private static func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account]
    }

    static func get(_ account: String) throws -> Data? {
        var q = query(account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &out)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw Failure(status: status) }
        return out as? Data
    }

    /// Adds or replaces the item; `accessible` is one of the `kSecAttrAccessible…ThisDeviceOnly` values.
    static func set(_ data: Data, account: String, accessible: CFString) throws {
        let attrs: [String: Any] = [kSecValueData as String: data, kSecAttrAccessible as String: accessible]
        var status = SecItemUpdate(query(account) as CFDictionary, attrs as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(query(account).merging(attrs) { $1 } as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw Failure(status: status) }
    }

    static func delete(_ account: String) throws {
        let status = SecItemDelete(query(account) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw Failure(status: status) }
    }
}
