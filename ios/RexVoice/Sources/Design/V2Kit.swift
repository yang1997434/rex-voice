import SwiftUI
import UIKit

// Second-round drafts (D「房间」/ E「卡片」): the persona is the protagonist, the surfaces are
// gradient + light + grain instead of plain system glass.

/// Which Claude look the user picked. Images live in `Assets.xcassets/Personas/<style>/<state>`.
enum PersonaStyle: String, CaseIterable, Identifiable {
    case pixel, chibi, watercolor, neon
    var id: String { rawValue }

    static let storageKey = "v2.persona"

    var title: String {
        switch self {
        case .pixel: "像素"
        case .chibi: "Q 版"
        case .watercolor: "水彩"
        case .neon: "霓虹"
        }
    }

    var blurb: String {
        switch self {
        case .pixel: "原版半身立绘"
        case .chibi: "圆滚滚的小只"
        case .watercolor: "纸纹和晕染"
        case .neon: "青紫霓虹边光"
        }
    }

    /// False while a group's art has not landed yet; the carousel shows it as「待补」.
    var available: Bool { UIImage(named: Self.name(self, .idle)) != nil }

    static func name(_ style: PersonaStyle, _ state: PersonaState) -> String {
        "Personas/\(style.rawValue)/\(state.rawValue)"
    }

    /// Falls back to the pixel group for any state a group is missing.
    static func resolved(_ style: PersonaStyle, _ state: PersonaState) -> String {
        let n = name(style, state)
        return UIImage(named: n) != nil ? n : name(.pixel, state)
    }

    #if DEBUG
    /// `REXVOICE_PERSONA=chibi` picks the group for screenshots.
    static func applyPreviewOverride() {
        if let raw = ProcessInfo.processInfo.environment["REXVOICE_PERSONA"], PersonaStyle(rawValue: raw) != nil {
            UserDefaults.standard.set(raw, forKey: storageKey)
        }
    }
    #endif
}

/// The five expressions every group draws.
enum PersonaState: String, CaseIterable {
    case idle, listening, thinking, speaking, offline

    init(_ phase: CallPhase) {
        switch phase {
        case .unpaired, .connecting, .idle: self = .idle
        case .offline: self = .offline
        case .dialing, .listening: self = .listening
        case .thinking: self = .thinking
        case .speaking: self = .speaking
        }
    }
}

/// The persona image: 300 ms crossfade between states, a slow breath while speaking.
struct PersonaView: View {
    let style: PersonaStyle
    let state: PersonaState
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let name = PersonaStyle.resolved(style, state)
        TimelineView(.animation(paused: state != .speaking || reduceMotion)) { ctx in
            let t = ctx.date.timeIntervalSinceReferenceDate
            let breath = state == .speaking && !reduceMotion ? 1 + 0.018 * sin(t * 2 * .pi / 2.4) : 1
            ZStack {
                Image(name)
                    .resizable()
                    .scaledToFit()
                    .id(name)
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .scale(scale: 0.97, anchor: .bottom)))
            }
            .scaleEffect(breath, anchor: .bottom)
        }
        .animation(.easeInOut(duration: 0.3), value: name)
        .accessibilityElement()
        .accessibilityLabel("Claude 形象，\(style.title)")
    }
}

/// Stand-in mic level until AudioEngine exposes one (implementation brief): a lively wobble.
func fakeLevel(_ t: TimeInterval) -> Double {
    min(1, 0.25 + 0.75 * abs(sin(t * 2.3)) * (0.55 + 0.45 * abs(sin(t * 0.7 + 1))))
}

/// Rings of light around the persona that swell with the voice while listening.
struct VoiceHalo: View {
    var active: Bool
    var color: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(paused: !active || reduceMotion)) { ctx in
            let level = active ? (reduceMotion ? 0.5 : fakeLevel(ctx.date.timeIntervalSinceReferenceDate)) : 0
            GeometryReader { geo in
                let d = min(geo.size.width, geo.size.height)
                ZStack {
                    Circle()
                        .fill(RadialGradient(colors: [color.opacity(0.55 * level), .clear],
                                             center: .center, startRadius: d * 0.1, endRadius: d * 0.55))
                    ForEach(0..<3, id: \.self) { i in
                        Circle()
                            .stroke(color.opacity((0.5 - Double(i) * 0.14) * level), lineWidth: 1.5)
                            .frame(width: d * (0.72 + Double(i) * 0.13 + 0.08 * level),
                                   height: d * (0.72 + Double(i) * 0.13 + 0.08 * level))
                    }
                }
                .frame(width: geo.size.width, height: geo.size.height)
            }
        }
        .opacity(active ? 1 : 0)
        .animation(.easeInOut(duration: 0.3), value: active)
        .accessibilityHidden(true)
    }
}

/// Film grain: one 128² noise tile made once, tiled and blended over a surface.
@MainActor
enum Grain {
    static let tile: UIImage = {
        let side = 128
        var g = SystemRandomNumberGenerator()
        var bytes = [UInt8](repeating: 0, count: side * side)
        for i in bytes.indices { bytes[i] = UInt8.random(in: 0...255, using: &g) }
        let provider = CGDataProvider(data: Data(bytes) as CFData)!
        let cg = CGImage(width: side, height: side, bitsPerComponent: 8, bitsPerPixel: 8, bytesPerRow: side,
                         space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGBitmapInfo(rawValue: 0),
                         provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent)!
        return UIImage(cgImage: cg, scale: 2, orientation: .up)
    }()
}

struct GrainOverlay: View {
    var opacity = 0.07
    var body: some View {
        Image(uiImage: Grain.tile)
            .resizable(resizingMode: .tile)
            .blendMode(.overlay)
            .opacity(opacity)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// Light colors per state: the scene's base gradient and the glow behind the persona.
struct Mood {
    let top: Color
    let bottom: Color
    let glow: Color
    let accent: Color

    init(_ state: PersonaState, dark: Bool) {
        func c(_ hex: UInt32) -> Color {
            Color(red: Double(hex >> 16 & 0xFF) / 255, green: Double(hex >> 8 & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
        }
        switch (state, dark) {
        case (.idle, true): (top, bottom, glow, accent) = (c(0x0D1322), c(0x2B2A44), c(0xF0A07E), c(0xF5B391))
        case (.idle, false): (top, bottom, glow, accent) = (c(0xB9CDE6), c(0xF4DCCF), c(0xFFB08A), c(0xD9774F))
        case (.listening, true): (top, bottom, glow, accent) = (c(0x071A1D), c(0x113A3A), c(0x3DD6B5), c(0x5EE6C6))
        case (.listening, false): (top, bottom, glow, accent) = (c(0xBFE3E0), c(0xE6F2E8), c(0x6EE2C5), c(0x1E9E83))
        case (.thinking, true): (top, bottom, glow, accent) = (c(0x100C26), c(0x2C2152), c(0x9B7DFF), c(0xB49CFF))
        case (.thinking, false): (top, bottom, glow, accent) = (c(0xCFC8EE), c(0xEFE4F4), c(0xB8A2FF), c(0x6C4FD8))
        case (.speaking, true): (top, bottom, glow, accent) = (c(0x1C1209), c(0x46290F), c(0xFFB35E), c(0xFFC27A))
        case (.speaking, false): (top, bottom, glow, accent) = (c(0xF3D7B8), c(0xFBEADB), c(0xFFC07A), c(0xC2680F))
        case (.offline, true): (top, bottom, glow, accent) = (c(0x0F1013), c(0x1E2026), c(0x5B6270), c(0x9AA1AE))
        case (.offline, false): (top, bottom, glow, accent) = (c(0xD5D8DE), c(0xE9E9EC), c(0xA7ADB8), c(0x5B6270))
        }
    }
}

/// Deep gradient + soft blurred light pools + grain: the「房间」 the persona stands in.
struct AmbientBackground: View {
    let mood: Mood
    var glowCenter = UnitPoint(x: 0.5, y: 0.36)

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width, h = geo.size.height
            ZStack {
                LinearGradient(colors: [mood.top, mood.bottom], startPoint: .top, endPoint: .bottom)
                Circle().fill(mood.glow.opacity(0.55))
                    .frame(width: w * 1.1, height: w * 1.1)
                    .blur(radius: 90)
                    .position(x: w * glowCenter.x, y: h * glowCenter.y)
                Circle().fill(mood.glow.opacity(0.25))
                    .frame(width: w * 0.8, height: w * 0.8)
                    .blur(radius: 110)
                    .position(x: w * 0.05, y: h * 0.92)
                Circle().fill(mood.accent.opacity(0.18))
                    .frame(width: w * 0.7, height: w * 0.7)
                    .blur(radius: 100)
                    .position(x: w * 1.0, y: h * 0.08)
                GrainOverlay(opacity: 0.08)
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}

/// Press feedback: 0.96 over 150 ms, none when disabled.
struct PressScale: ButtonStyle {
    @Environment(\.isEnabled) private var enabled
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && enabled ? 0.96 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// The one main button: 配对 / 开始通话 / 挂断, or a dim「等 Mac 上线」 while it cannot call.
struct V2CallButton: View {
    let phase: CallPhase
    let enabled: Bool
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let (title, symbol): (String, String) = switch phase {
        case .unpaired: ("配对 Mac", "qrcode.viewfinder")
        case .connecting, .offline: ("等 Mac 上线", "hourglass")
        case .idle: ("开始通话", "phone.fill")
        default: ("挂断", "phone.down.fill")
        }
        let hangUp = phase.inCall
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: symbol)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(hangUp ? .white : (phase == .idle ? Color.green : .primary))
                    .contentTransition(.symbolEffect(.replace))
                Text(title).font(.headline)
            }
            .foregroundStyle(hangUp ? .white : .primary)
            .frame(maxWidth: .infinity, minHeight: 58)
            .background {
                Capsule().fill(hangUp ? AnyShapeStyle(Color.red.gradient)
                               : AnyShapeStyle(scheme == .dark ? Color.white.opacity(0.14) : Color.white.opacity(0.85)))
            }
            .overlay {
                Capsule().strokeBorder(LinearGradient(colors: [.white.opacity(scheme == .dark ? 0.28 : 0.9), .white.opacity(0.04)],
                                                      startPoint: .top, endPoint: .bottom), lineWidth: 1)
            }
            .shadow(color: (hangUp ? Color.red : .black).opacity(scheme == .dark ? 0.35 : 0.12), radius: 16, y: 8)
            .contentShape(.capsule)
        }
        .buttonStyle(PressScale())
        .disabled(!enabled)
        .opacity(enabled ? 1 : 0.55)
        .animation(.smooth(duration: 0.3), value: phase.inCall)
    }
}

/// Round icon button used in both drafts' top bars.
struct V2IconButton: View {
    let title: String
    let symbol: String
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.body.weight(.medium))
                .frame(width: 44, height: 44)
                .background(Circle().fill(scheme == .dark ? Color.white.opacity(0.1) : Color.white.opacity(0.6)))
                .overlay(Circle().strokeBorder(.white.opacity(scheme == .dark ? 0.12 : 0.7), lineWidth: 1))
        }
        .buttonStyle(PressScale())
        .foregroundStyle(.primary)
        .accessibilityLabel(title)
    }
}

/// Serif wordmark, Hark-style.
struct Wordmark: View {
    var body: some View {
        Text("Rex Voice").font(.system(.title3, design: .serif).weight(.semibold))
    }
}

// MARK: - Voice catalog (layout only; the real lists come with the provider brief)

struct VoiceOption: Identifiable, Hashable {
    let name: String
    let value: String
    let blurb: String
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

    /// The Gemini list is a placeholder until the provider research is checked.
    var unverified: Bool { self == .gemini }

    /// Total voices when the list above is only the first part (Grok has 28); shows a「自定义」row.
    var moreVoices: Int? { self == .grok ? 28 : nil }

    // shortcut: model lists beyond the Go defaults (internal/voice/voice.go) are placeholders; the provider brief fixes them.
    var models: [String] {
        switch self {
        case .openai: ["gpt-realtime-2.1", "gpt-realtime-mini"]
        case .qwen: ["qwen3.8-omni-flash-realtime", "qwen3-omni-flash-realtime"]
        case .gemini: ["gemini-3.8-live"]
        case .grok: ["grok-voice-3.0"]
        }
    }

    var voices: [VoiceOption] {
        func v(_ n: String, _ b: String, _ value: String? = nil) -> VoiceOption { VoiceOption(name: n, value: value ?? n, blurb: b) }
        switch self {
        case .openai:
            return [v("Marin", "温和自然的女声，像身边的同事", "marin"), v("Cedar", "沉稳低缓的男声", "cedar"),
                    v("Alloy", "中性清亮，不偏男女", "alloy"), v("Ash", "干脆利落的男声", "ash"),
                    v("Ballad", "柔和，带点英伦腔", "ballad"), v("Coral", "明亮热情的女声", "coral"),
                    v("Echo", "平稳清晰的男声", "echo"), v("Sage", "安静知性的女声", "sage"),
                    v("Shimmer", "轻快柔亮的女声", "shimmer"), v("Verse", "有表现力，像在讲故事", "verse")]
        case .qwen:
            return [v("Tina", "亲切利落的女声"), v("Liora Mira", "温柔细腻的女声"), v("Raymond", "成熟稳重的男声"),
                    v("Ryan", "阳光有活力的男声"), v("Mia", "甜美轻快的女声"), v("Serena", "优雅舒缓的女声"),
                    v("Evan", "清爽的少年感男声"), v("Momo", "俏皮可爱的女声"), v("Zane", "低沉有磁性的男声（3.8 新增）"),
                    v("Cici", "元气满满的女声（3.8 新增）")]
        case .gemini:
            return [v("Puck", "轻快俏皮"), v("Charon", "沉稳，播报感"), v("Kore", "坚定清晰"), v("Fenrir", "激昂有力"),
                    v("Aoede", "轻盈明快"), v("Leda", "年轻清新"), v("Orus", "稳重厚实"), v("Zephyr", "明亮通透")]
        case .grok:
            // Real list from the Grok API (2026-11-01, 28 voices); the first ten, the rest behind「自定义」.
            return [v("Ara", "女声，默认", "ara"), v("Eve", "女声", "eve"), v("Aurora", "女声", "aurora"),
                    v("Luna", "女声", "luna"), v("Iris", "女声", "iris"), v("Celeste", "女声", "celeste"),
                    v("Rex", "男声", "rex"), v("Leo", "男声", "leo"), v("Orion", "男声", "orion"), v("Atlas", "男声", "atlas")]
        }
    }
}

/// Small brand block: rounded square in the provider's color with its letter.
struct BrandMark: View {
    let provider: VoiceProvider
    var size: CGFloat = 22
    var body: some View {
        Text(provider.mark)
            .font(.system(size: size * 0.55, weight: .bold, design: .rounded))
            .foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(RoundedRectangle(cornerRadius: size * 0.3).fill(provider.brand))
            .overlay(RoundedRectangle(cornerRadius: size * 0.3).strokeBorder(.white.opacity(0.18), lineWidth: 0.5))
            .accessibilityHidden(true)
    }
}
