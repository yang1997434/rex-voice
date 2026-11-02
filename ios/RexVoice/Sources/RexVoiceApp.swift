import SwiftUI

@main
struct RexVoiceApp: App {
    @State private var settings: AppSettings
    @State private var controller: CallController

    init() {
        let s = AppSettings()
        #if DEBUG
        Self.applyPreviewSettings(s)
        #endif
        let c = CallController(settings: s)
        #if DEBUG
        if let screen = PreviewScreen.current { c.applyPreview(screen) }
        #endif
        _settings = State(initialValue: s)
        _controller = State(initialValue: c)
    }

    var body: some Scene {
        WindowGroup {
            root.environment(controller).environment(settings)
        }
    }

    @ViewBuilder private var root: some View {
        #if DEBUG
        ContentView(initialSheet: PreviewScreen.current.flatMap { ContentView.Sheet(rawValue: $0.rawValue) })
        #else
        ContentView()
        #endif
    }

    #if DEBUG
    /// Screenshot drivers; each writes the real (persisted) setting, so a later plain launch shows it too.
    /// `REXVOICE_PERSONA=<id>`, `REXVOICE_PROVIDER=openai|qwen|gemini|grok`, `REXVOICE_MODEL` / `REXVOICE_VOICE=<name>`,
    /// `REXVOICE_KEY=<key>` saves a key for the provider (`-` deletes it).
    private static func applyPreviewSettings(_ s: AppSettings) {
        let env = ProcessInfo.processInfo.environment
        if let id = env["REXVOICE_PERSONA"] { s.personaID = id }
        if let p = env["REXVOICE_PROVIDER"].flatMap(VoiceProvider.init(rawValue:)) { s.provider = p }
        if let m = env["REXVOICE_MODEL"] { s.setModel(m, for: s.provider) }
        if let v = env["REXVOICE_VOICE"] { s.setVoice(v, for: s.provider) }
        if let key = env["REXVOICE_KEY"] {
            do {
                if key == "-" { try s.deleteKey(for: s.provider) } else { try s.saveKey(key, for: s.provider) }
            } catch {
                print("REXVOICE_KEY failed: \(error.localizedDescription)") // the error text never contains the key
            }
        }
    }
    #endif
}
