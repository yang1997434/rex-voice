import Foundation
import Observation
import RexVoiceCore
import SwiftUI
import UIKit

/// What the phone picks for a call: provider, per-provider model / voice / API key, and the persona.
/// Plain values live in UserDefaults; keys live in the Keychain (`apikey.<provider>`), never in memory beyond a call.
@MainActor
@Observable
final class AppSettings {
    /// Switching provider puts its model and voice back to that provider's defaults (2026-11-01 user call:
    /// a fresh install only needs the API key to make a call).
    var provider: VoiceProvider {
        didSet {
            defaults.set(provider.rawValue, forKey: "provider")
            guard provider != oldValue else { return }
            defaults.removeObject(forKey: "model.\(provider.rawValue)")
            defaults.removeObject(forKey: "voice.\(provider.rawValue)")
            revision += 1
        }
    }
    var personaID: String {
        didSet { defaults.set(personaID, forKey: Self.personaKey) }
    }
    /// Last four characters of each saved key, for「已保存 ••••1234」; the key itself is read only when a call starts.
    private(set) var keyTails: [VoiceProvider: String] = [:]
    /// Bumped on model / voice writes so views reading them refresh (they are stored in UserDefaults, not here).
    private var revision = 0

    @ObservationIgnored private let defaults = UserDefaults.standard
    static let personaKey = "persona"

    init() {
        provider = defaults.string(forKey: "provider").flatMap(VoiceProvider.init(rawValue:)) ?? .openai
        personaID = defaults.string(forKey: Self.personaKey) ?? ""
        for p in VoiceProvider.allCases {
            if let key = try? Self.storedKey(p) { keyTails[p] = String(key.suffix(4)) }
        }
    }

    func model(for p: VoiceProvider) -> String {
        _ = revision
        return defaults.string(forKey: "model.\(p.rawValue)") ?? p.models[0]
    }

    func voice(for p: VoiceProvider) -> String {
        _ = revision
        return defaults.string(forKey: "voice.\(p.rawValue)") ?? p.voices(model: model(for: p))[0].value
    }

    /// A voice the new model's list does not have goes back to that list's default.
    func setModel(_ m: String, for p: VoiceProvider) {
        let voice = voice(for: p)
        defaults.set(m, forKey: "model.\(p.rawValue)")
        if !p.voices(model: m).contains(where: { $0.value == voice }) {
            defaults.removeObject(forKey: "voice.\(p.rawValue)")
        }
        revision += 1
    }

    func setVoice(_ v: String, for p: VoiceProvider) {
        defaults.set(v, forKey: "voice.\(p.rawValue)")
        revision += 1
    }

    // MARK: API keys

    private static func account(_ p: VoiceProvider) -> String { "apikey.\(p.rawValue)" }

    private static func storedKey(_ p: VoiceProvider) throws -> String? {
        try Keychain.get(account(p)).flatMap { String(data: $0, encoding: .utf8) }
    }

    /// The saved key for a provider, read from the Keychain at call start.
    func apiKey(for p: VoiceProvider) throws -> String? { try Self.storedKey(p) }

    func saveKey(_ raw: String, for p: VoiceProvider) throws {
        let key = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty else { return }
        try Keychain.set(Data(key.utf8), account: Self.account(p), accessible: kSecAttrAccessibleWhenUnlockedThisDeviceOnly)
        keyTails[p] = String(key.suffix(4))
    }

    func deleteKey(for p: VoiceProvider) throws {
        try Keychain.delete(Self.account(p))
        keyTails[p] = nil
        #if DEBUG
        assert((try? Keychain.get(Self.account(p))) == nil, "API key still in the Keychain after delete")
        #endif
    }

    /// The「还没填 key」 prompt shows once per provider; after that the Mac's own fallback key is assumed fine.
    func shouldPromptForKey(_ p: VoiceProvider) -> Bool {
        keyTails[p] == nil && !defaults.bool(forKey: "keyPrompted.\(p.rawValue)")
    }

    func markKeyPrompted(_ p: VoiceProvider) { defaults.set(true, forKey: "keyPrompted.\(p.rawValue)") }

    /// What `call.start` carries for the current provider.
    func callStart() throws -> ControlMessage {
        .callStart(provider: provider.rawValue, model: model(for: provider), voice: voice(for: provider),
                   apiKey: try apiKey(for: provider))
    }

    // MARK: Persona

    /// The picked persona, or the first available one when the stored id has no art (removed group, first launch).
    var persona: Persona {
        let all = Persona.available
        return all.first { $0.id == personaID } ?? all.first ?? Persona.placeholder
    }
}

// MARK: - Personas (data-driven: Resources/personas.json + Assets.xcassets/Personas/<id>/<state>)

struct Persona: Decodable, Identifiable, Hashable {
    let id: String
    let name: String
    var blurb: String?
    /// Card tint in the carousel, `#RRGGBB`.
    var tint: String?
    let source: String
    let license: String

    static let placeholder = Persona(id: "", name: "Claude", source: "", license: "")

    /// Every entry in personas.json, in file order.
    static let all: [Persona] = {
        guard let url = Bundle.main.url(forResource: "personas", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let list = try? JSONDecoder().decode([Persona].self, from: data)
        else {
            assertionFailure("personas.json missing or malformed")
            return []
        }
        return list
    }()

    /// Entries whose five expressions are all in the asset catalog; a group with missing art is skipped.
    static let available: [Persona] = all.filter { p in
        PersonaState.allCases.allSatisfy { UIImage(named: p.image($0)) != nil }
    }

    func image(_ state: PersonaState) -> String { "Personas/\(id)/\(state.rawValue)" }

    var tintColor: Color {
        guard let tint, tint.hasPrefix("#"), let v = UInt32(tint.dropFirst(), radix: 16) else { return .gray }
        return Color(red: Double(v >> 16 & 0xFF) / 255, green: Double(v >> 8 & 0xFF) / 255, blue: Double(v & 0xFF) / 255)
    }
}

// MARK: - Provider catalog (built in; the Mac validates names, a「自定义…」 row covers anything missing here)

struct VoiceOption: Identifiable, Hashable {
    let name: String
    let value: String
    /// Only Grok's voice list says male / female; nil elsewhere.
    var gender: String?
    var id: String { value }
}

enum VoiceProvider: String, CaseIterable, Identifiable {
    case openai, qwen, gemini, grok
    var id: String { rawValue }

    var title: String {
        switch self {
        case .openai: "OpenAI"
        case .qwen: "通义 Qwen"
        case .gemini: "Gemini"
        case .grok: "Grok"
        }
    }

    /// Letter on the brand-colored block (no logo art in the app).
    var mark: String {
        switch self {
        case .openai: "O"
        case .qwen: "Q"
        case .gemini: "G"
        case .grok: "X"
        }
    }

    var brand: LinearGradient {
        let colors: [Color] = switch self {
        case .openai: [Color(red: 0.06, green: 0.64, blue: 0.5), Color(red: 0.03, green: 0.45, blue: 0.36)]
        case .qwen: [Color(red: 0.43, green: 0.36, blue: 0.98), Color(red: 0.31, green: 0.24, blue: 0.85)]
        case .gemini: [Color(red: 0.26, green: 0.52, blue: 0.96), Color(red: 0.61, green: 0.4, blue: 0.95)]
        case .grok: [Color(white: 0.22), Color(white: 0.05)]
        }
        return LinearGradient(colors: colors, startPoint: .topLeading, endPoint: .bottomTrailing)
    }

    /// First entry is the default (same as the Mac's `voice.Defaults`).
    var models: [String] {
        switch self {
        case .openai: ["gpt-realtime-2.1", "gpt-realtime-2.1-mini", "gpt-live-1"]
        case .qwen: ["qwen-audio-3.1-realtime-plus", "qwen3.8-omni-flash-realtime", "qwen-audio-3.1-realtime-flash",
                     "qwen3.5-omni-plus-realtime"]
        case .gemini: ["gemini-3.8-live", "gemini-3.8-live-extended-thinking"]
        case .grok: ["grok-voice-3.0"]
        }
    }

    /// How many voices the provider has in all, when the list below is only the common ones.
    var totalVoices: Int? { self == .gemini ? 30 : nil }

    /// Voices for a model; first entry is the default. Qwen's qwen-audio-3.1 models have their own set (Mac 0770c9d).
    func voices(model: String) -> [VoiceOption] {
        func v(_ value: String, _ name: String? = nil, _ gender: String? = nil) -> VoiceOption {
            VoiceOption(name: name ?? value, value: value, gender: gender)
        }
        switch self {
        case .openai:
            return ["marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse"]
                .map { v($0, $0.capitalized) }
        case .qwen where model.hasPrefix("qwen-audio-3.1"):
            return ["longanqian", "longanlingxi", "longanfengyue", "luna_v3.1", "emily_v3.1", "andy_v3.1", "eric_v3.1",
                    "libai_v3.1"].map { v($0) }
        case .qwen:
            return ["Tina", "Liora Mira", "Raymond", "Ryan", "Mia", "Serena", "Evan", "Momo", "Zane", "Cici"].map { v($0) }
        case .gemini:
            return ["Kore", "Puck", "Charon", "Fenrir", "Leda", "Orus", "Aoede", "Zephyr", "Callirrhoe", "Despina", "Achird",
                    "Sulafat"].map { v($0) }
        case .grok:
            // GET /v1/tts/voices, 2026-11-01: 28 ids. Gender is only known for the 14 named in the research notes.
            let female = ["ara", "eve", "aurora", "carina", "celeste", "iris", "liora", "luna"]
            let male = ["altair", "atlas", "castor", "leo", "orion", "rex"]
            let other = ["cosmo", "helios", "helix", "kepler", "lumen", "lux", "naksh", "perseus", "rigel", "sal", "sirius",
                         "ursa", "zagan", "zenith"]
            return female.map { v($0, $0.capitalized, "女声") } + male.map { v($0, $0.capitalized, "男声") }
                + other.map { v($0, $0.capitalized) }
        }
    }
}
