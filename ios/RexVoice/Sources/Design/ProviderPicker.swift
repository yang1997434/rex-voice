import SwiftUI

// Design H (final 2026-11-02): the provider is picked from its logo in the main screen's rail.

/// Stand-in logo: a brand-colored disc with a white simplified glyph (OpenAI six-petal knot, Qwen「Q」,
/// Gemini sparkle, Grok「X」). shortcut: self-drawn marks, swap in the official art once the user supplies it.
struct ProviderLogo: View {
    let provider: VoiceProvider
    var size: CGFloat = 36

    var body: some View {
        let g = size * 0.66 // glyph box: 24pt on the 36pt disc the 56pt rail uses
        ZStack {
            Circle().fill(provider.brand)
            Circle().strokeBorder(.white.opacity(0.18), lineWidth: 0.5)
            glyph(g).frame(width: g, height: g)
        }
        .foregroundStyle(.white)
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    @ViewBuilder private func glyph(_ g: CGFloat) -> some View {
        switch provider {
        case .openai:
            ZStack {
                ForEach(0..<3, id: \.self) { i in
                    Ellipse().stroke(lineWidth: g * 0.1)
                        .frame(width: g * 0.42, height: g * 0.95)
                        .rotationEffect(.degrees(Double(i) * 60))
                }
            }
        case .qwen:
            Text("Q").font(.system(size: g * 0.95, weight: .heavy, design: .rounded))
        case .gemini:
            Sparkle()
        case .grok:
            Path { p in
                p.move(to: CGPoint(x: g * 0.18, y: g * 0.12)); p.addLine(to: CGPoint(x: g * 0.82, y: g * 0.88))
                p.move(to: CGPoint(x: g * 0.82, y: g * 0.12)); p.addLine(to: CGPoint(x: g * 0.18, y: g * 0.88))
            }
            .stroke(style: StrokeStyle(lineWidth: g * 0.14, lineCap: .round))
        }
    }
}

/// Four-point star with concave sides.
private struct Sparkle: Shape {
    func path(in r: CGRect) -> Path {
        let c = CGPoint(x: r.midX, y: r.midY)
        var p = Path()
        p.move(to: CGPoint(x: r.midX, y: r.minY))
        p.addQuadCurve(to: CGPoint(x: r.maxX, y: r.midY), control: c)
        p.addQuadCurve(to: CGPoint(x: r.midX, y: r.maxY), control: c)
        p.addQuadCurve(to: CGPoint(x: r.minX, y: r.midY), control: c)
        p.addQuadCurve(to: CGPoint(x: r.midX, y: r.minY), control: c)
        return p
    }
}

/// The four logos; the current provider has a fill + outline. Tapping one is the same as picking it on the
/// settings page. During a call it is faded and a tap only shakes it (the voice cannot change mid-call).
struct ProviderPicker: View {
    var axis: Axis = .vertical
    /// Square hit area per logo.
    var cell: CGFloat = 48
    @Environment(AppSettings.self) private var settings
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var shakes = 0

    var body: some View {
        let locked = controller.inCall
        let stack = axis == .vertical ? AnyLayout(VStackLayout(spacing: 6)) : AnyLayout(HStackLayout(spacing: 4))
        let amp: CGFloat = reduceMotion ? 0 : 6
        stack {
            ForEach(VoiceProvider.allCases) { p in
                let selected = settings.provider == p
                let shape = RoundedRectangle(cornerRadius: cell * 0.32, style: .continuous)
                Button {
                    if locked { shakes += 1 } else { settings.provider = p }
                } label: {
                    ProviderLogo(provider: p, size: cell * 0.75)
                        .frame(width: cell, height: cell)
                        .background(shape.fill(selected ? Color.primary.opacity(scheme == .dark ? 0.16 : 0.08) : .clear))
                        .overlay(shape.strokeBorder(Color.primary.opacity(selected ? 0.6 : 0), lineWidth: 1.5))
                        .contentShape(shape)
                }
                .buttonStyle(PressScale())
                .accessibilityLabel(p.title)
                .accessibilityAddTraits(selected ? .isSelected : [])
                .accessibilityHint(locked ? "通话中不能换" : "")
            }
        }
        .opacity(locked ? 0.4 : 1)
        .keyframeAnimator(initialValue: CGFloat(0), trigger: shakes) { content, x in
            content.offset(x: x)
        } keyframes: { _ in
            KeyframeTrack {
                LinearKeyframe(amp, duration: 0.05)
                LinearKeyframe(-amp, duration: 0.08)
                LinearKeyframe(amp * 0.6, duration: 0.07)
                LinearKeyframe(0, duration: 0.06)
            }
        }
        .sensoryFeedback(.error, trigger: shakes)
        .animation(.smooth(duration: 0.25), value: settings.provider)
        .animation(.smooth(duration: 0.25), value: locked)
    }
}
