import SwiftUI

/// The rail shows only the current provider's logo with its short name under it. Holding it 0.35 s fans the other
/// three out to the upper left (105° / 150° / 195°, 60pt from its center, so the backdrop clears the screen edge and
/// the pairing / settings buttons below); slide onto one and let go to pick it. While open the current one wears an
/// accent ring and the check badge sits on whichever a let-go would pick. Letting go on the
/// center keeps the fan open so each logo can be tapped (VoiceOver too); tapping the center again or anywhere else
/// (`open` is bound to the screen's tap catcher) closes it. A short tap shakes and shows「长按换供应商」; during a call
/// a hold only shakes (the voice cannot change mid-call).
struct ProviderWheel: View {
    @Binding var open: Bool
    var cell: CGFloat = 48
    @Environment(AppSettings.self) private var settings
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Fan logo under the finger while dragging.
    @State private var hover: VoiceProvider?
    @State private var shakes = 0
    @State private var hint = false
    /// The current touch: when it started, whether the fan was already open then, and the pending 0.35 s timer.
    @State private var touchStart: Date?
    @State private var wasOpen = false
    @State private var holdTimer: Task<Void, Never>?
    /// Bumped on every pick: the name under the logo stands out for 1.6 s.
    @State private var picks = 0
    @State private var justPicked = false

    private static let radius: CGFloat = 60
    private static let logo: CGFloat = 44
    private static let fanLogo: CGFloat = 36

    /// Fan slots, top to bottom: 105°, 150°, 195° (screen y points down).
    private static let slots: [CGSize] = [105.0, 150, 195].map {
        let a = $0 * .pi / 180
        return CGSize(width: radius * cos(a), height: -radius * sin(a))
    }

    /// Backdrop: the smallest circle around the three fan logos plus an 8pt margin. The slots span 90°, so the
    /// end two are its diameter (the middle one lies on it).
    private static let backdropCenter = CGSize(width: (slots[0].width + slots[2].width) / 2,
                                               height: (slots[0].height + slots[2].height) / 2)
    private static let backdropRadius = hypot(slots[0].width - slots[2].width, slots[0].height - slots[2].height) / 2
        + fanLogo / 2 + 8

    var body: some View {
        let locked = controller.inCall
        let others = VoiceProvider.allCases.filter { $0 != settings.provider }
        let amp: CGFloat = reduceMotion ? 0 : 5
        let accent = Mood(PersonaState(controller.phase), dark: scheme == .dark).accent
        VStack(spacing: 2) {
            wheel(locked: locked, others: others, amp: amp, accent: accent)
            Text(settings.provider.shortName)
                .font(.system(size: justPicked ? 10 : 9, weight: justPicked ? .semibold : .regular))
                .foregroundStyle(justPicked ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .contentTransition(.opacity)
                .accessibilityHidden(true) // the logo's label says it
        }
        .opacity(locked ? 0.4 : 1)
        .task(id: picks) {
            guard picks > 0 else { return }
            withAnimation(.smooth(duration: 0.2)) { justPicked = true }
            try? await Task.sleep(for: .seconds(1.6))
            if !Task.isCancelled { withAnimation(.smooth(duration: 0.3)) { justPicked = false } }
        }
        .animation(.smooth(duration: 0.25), value: locked)
    }

    private func wheel(locked: Bool, others: [VoiceProvider], amp: CGFloat, accent: Color) -> some View {
        ProviderLogo(provider: settings.provider, size: Self.logo)
            .frame(width: cell, height: cell)
            .overlay { // the current one while open: accent ring, and the check while no fan logo is under the finger
                if open {
                    Circle().strokeBorder(accent, lineWidth: 2)
                        .overlay(alignment: .bottomTrailing) { if hover == nil { CheckBadge(accent: accent) } }
                        .transition(.opacity)
                }
            }
            .contentShape(.circle)
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
            .gesture(press(locked: locked, others: others))
            .accessibilityElement()
            .accessibilityLabel("当前：\(settings.provider.shortName)")
            .accessibilityHint(locked ? "通话中不能换" : (open ? "已展开，选一家，或再点一下收起" : "长按换供应商"))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { // VoiceOver double-tap opens / closes the fan directly
                if locked { shakes += 1 } else { open.toggle() }
            }
            .background { backdrop } // under the center logo, which stays visible at the fan's lower right
            .overlay { fan(others, accent: accent) }
            .overlay(alignment: .trailing) {
                if hint {
                    Text("长按换供应商")
                        .font(.footnote.weight(.semibold))
                        .fixedSize()
                        .padding(.horizontal, 12)
                        .frame(minHeight: 32)
                        .glassEffect(.regular, in: .capsule)
                        .offset(x: -(cell + 8)) // trailing edge 8pt left of the logo
                        .transition(.opacity)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            .task(id: hint) {
                guard hint else { return }
                try? await Task.sleep(for: .seconds(1.6))
                if !Task.isCancelled { withAnimation { hint = false } }
            }
            .sensoryFeedback(trigger: shakes) { _, _ in locked ? .error : .impact(weight: .light) }
            .sensoryFeedback(trigger: open) { _, now in now ? .impact(weight: .medium) : nil }
            .sensoryFeedback(trigger: hover) { _, now in now != nil ? .selection : nil }
            .onChange(of: locked) { _, now in if now { close() } }
            .animation(.smooth(duration: 0.2), value: hover)
            .animation(.smooth(duration: 0.2), value: open)
    }

    private var backdrop: some View {
        let dark = scheme == .dark
        let size = Self.backdropRadius * 2
        return Circle()
            .fill(.regularMaterial)
            .overlay(Circle().strokeBorder(.white.opacity(dark ? 0.12 : 0.6), lineWidth: 1))
            .shadow(color: .black.opacity(dark ? 0.4 : 0.12), radius: 20, y: 8)
            .frame(width: size, height: size)
            .scaleEffect(open ? 1 : 0.3, anchor: UnitPoint(x: 0.5 - Self.backdropCenter.width / size,
                                                         y: 0.5 - Self.backdropCenter.height / size)) // grows out of the logo
            .offset(Self.backdropCenter)
            .opacity(open ? 1 : 0)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
            .animation(reduceMotion ? nil : .spring(response: 0.32, dampingFraction: 0.72), value: open)
    }

    /// The other three, drawn over the screen to the left; they grow out of the center and only exist while open
    /// (so VoiceOver never lands on a hidden one).
    private func fan(_ others: [VoiceProvider], accent: Color) -> some View {
        ZStack {
            if open {
                ForEach(Array(others.enumerated()), id: \.element) { i, p in
                    let on = hover == p
                    let slot = Self.slots[i]
                    Button { pick(p) } label: {
                        ProviderLogo(provider: p, size: Self.fanLogo)
                            .padding(4)
                            .background(Circle().strokeBorder(Color.primary.opacity(on ? 0.7 : 0), lineWidth: 2))
                            .overlay(alignment: .bottomTrailing) { if on { CheckBadge(accent: accent) } }
                            .scaleEffect(on ? 1.18 : 1)
                    }
                    .buttonStyle(PressScale())
                    .accessibilityLabel(p.title)
                    .accessibilityHint("选用这家")
                    .offset(slot)
                    .transition(reduceMotion ? .opacity : .offset(x: -slot.width, y: -slot.height)
                        .combined(with: .scale(scale: 0.4)).combined(with: .opacity))
                }
            }
        }
        .animation(reduceMotion ? nil : .spring(response: 0.32, dampingFraction: 0.72), value: open)
        .animation(reduceMotion ? nil : .spring(response: 0.2, dampingFraction: 0.8), value: hover)
    }

    /// One gesture for hold / slide / let go, so a tap and a hold never fight over the same touch.
    private func press(locked: Bool, others: [VoiceProvider]) -> some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { v in
                let d = CGSize(width: v.location.x - cell / 2, height: v.location.y - cell / 2)
                if touchStart == nil {
                    touchStart = v.time
                    wasOpen = open
                    if !open {
                        holdTimer = Task {
                            try? await Task.sleep(for: .seconds(0.35))
                            guard !Task.isCancelled, touchStart != nil else { return }
                            if locked { shakes += 1 } else { open = true }
                        }
                    }
                }
                // Moving off before the hold fires is not a press.
                if !open, hypot(v.translation.width, v.translation.height) > 12 { holdTimer?.cancel() }
                if open { hover = target(d, others) }
            }
            .onEnded { v in
                holdTimer?.cancel()
                let d = CGSize(width: v.location.x - cell / 2, height: v.location.y - cell / 2)
                let held = touchStart.map { v.time.timeIntervalSince($0) } ?? 0
                let moved = hypot(v.translation.width, v.translation.height)
                touchStart = nil
                if open {
                    if let p = target(d, others) {
                        pick(p)
                    } else if hypot(d.width, d.height) > cell / 2 || wasOpen {
                        close() // let go off the fan, or tapped the center of an open fan
                    }
                    hover = nil // let go on the center right after opening: stays open for tapping
                } else if held < 0.35, moved <= 12 {
                    shakes += 1
                    if !locked { withAnimation { hint = true } }
                }
            }
    }

    /// The fan logo nearest the finger, if within 30pt (neighbors are 46pt apart).
    private func target(_ d: CGSize, _ others: [VoiceProvider]) -> VoiceProvider? {
        let near = zip(others, Self.slots).map { ($0, hypot(d.width - $1.width, d.height - $1.height)) }
            .min { $0.1 < $1.1 }
        return near.flatMap { $0.1 < 30 ? $0.0 : nil }
    }

    private func pick(_ p: VoiceProvider) {
        settings.provider = p
        picks += 1
        close()
    }

    private func close() {
        open = false
        hover = nil
    }
}

/// Small accent disc with a white check at a logo's lower right: the one a let-go picks.
private struct CheckBadge: View {
    let accent: Color
    var body: some View {
        Image(systemName: "checkmark")
            .font(.system(size: 8, weight: .heavy))
            .foregroundStyle(.white)
            .frame(width: 16, height: 16)
            .background(Circle().fill(accent))
            .overlay(Circle().strokeBorder(.white, lineWidth: 1.5))
            .offset(x: 3, y: 3)
            .transition(.scale.combined(with: .opacity))
            .accessibilityHidden(true)
    }
}
