import SwiftUI
import UIKit

// Design E「卡片」 kit: the persona is the protagonist, the surfaces are gradient + light + grain
// instead of plain system glass.

/// The five expressions every group draws.
enum PersonaState: String, CaseIterable {
    case idle, listening, thinking, speaking, offline

    init(_ phase: CallPhase) {
        switch phase {
        case .unpaired, .offline: self = .offline
        case .idle: self = .idle
        case .connecting, .dialing, .listening: self = .listening
        case .thinking: self = .thinking
        case .speaking: self = .speaking
        }
    }
}

/// The persona image: 300 ms crossfade between states, a slow breath while speaking.
struct PersonaView: View {
    let persona: Persona
    let state: PersonaState
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let name = persona.image(state)
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
        .accessibilityLabel("Claude 形象，\(persona.name)")
    }
}

/// Rings of light around the persona that swell with the mic level while listening.
struct VoiceHalo: View {
    var active: Bool
    /// `AudioEngine.level`, 0...1.
    var level: Double
    var color: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        // Reduce Motion: a steady halo instead of one that swells with the voice.
        let shown = active ? (reduceMotion ? 0.5 : level) : 0
        GeometryReader { geo in
            let d = min(geo.size.width, geo.size.height)
            ZStack {
                Circle()
                    .fill(RadialGradient(colors: [color.opacity(0.55 * shown), .clear],
                                         center: .center, startRadius: d * 0.1, endRadius: d * 0.55))
                ForEach(0..<3, id: \.self) { i in
                    Circle()
                        .stroke(color.opacity((0.5 - Double(i) * 0.14) * shown), lineWidth: 1.5)
                        .frame(width: d * (0.72 + Double(i) * 0.13 + 0.08 * shown),
                               height: d * (0.72 + Double(i) * 0.13 + 0.08 * shown))
                }
            }
            .frame(width: geo.size.width, height: geo.size.height)
        }
        .animation(.linear(duration: 0.05), value: shown)
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

/// Round icon button for the top bars.
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
