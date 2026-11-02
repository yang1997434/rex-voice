import SwiftUI

/// The rail shows only the current provider's logo with its short name under it. Holding it 0.35 s opens a dial
/// around it (second version, 2026-11-02): a frosted arc band of radius ~110pt on its upper left with the other three
/// logos on the band's center line, 40° apart at 120° / 160° / 200°. Without lifting the finger, turning it around the
/// logo turns the band with it (band angle = finger angle - angle where the finger left the center, spring-smoothed,
/// held between the angles that keep all three on screen, with a rubber band past them). The logo under the finger
/// (angle within 20°, finger over 40pt from the center) grows 1.5x; letting go there picks it: it flies to the center,
/// the old one flies to its slot and the dial closes. Letting go anywhere else closes it unchanged. A short tap shakes
/// and shows「长按换供应商」(and closes an open dial); during a call a hold only shakes.
struct ProviderWheel: View {
    @Binding var open: Bool
    var cell: CGFloat = 48
    /// Where the dial may draw, in global coordinates (the window minus the status bar / home indicator).
    var room: CGRect = .zero
    @Environment(AppSettings.self) private var settings
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Dial logo under the finger.
    @State private var hover: VoiceProvider?
    @State private var shakes = 0
    @State private var hint = false
    /// The current touch: when it started, whether the dial was open then, and the pending 0.35 s timer.
    @State private var touchStart: Date?
    @State private var holdTimer: Task<Void, Never>?
    /// Bumped on every pick: the name under the logo stands out for 1.6 s.
    @State private var picks = 0
    @State private var justPicked = false
    /// The three providers in the dial's slots (a pick swaps the old current one into the picked one's slot).
    @State private var order: [VoiceProvider] = []
    /// Band rotation in degrees (math direction, counterclockwise on screen): unclamped as the finger turned it, and
    /// as drawn (clamped with the rubber band).
    @State private var rawTurn: Double = 0
    @State private var turn: Double = 0
    /// Finger angle at the last update; nil while it is within 40pt of the center (angles jitter there).
    @State private var lastAngle: Double?
    /// The cell's center in global coordinates.
    @State private var center: CGPoint = .zero

    private static let logo: CGFloat = 44
    private static let band: CGFloat = 64
    private static let slots: [Double] = [120, 160, 200]
    /// Release / highlight tolerance: the finger's angle to a logo's.
    private static let pickTolerance: Double = 20
    /// Within this distance of the center the finger picks nothing and does not turn the band.
    private static let deadZone: CGFloat = 40
    /// Half of the 56pt rail (`ContentView.sideRail`): the pairing / settings buttons under the logo.
    private static let railHalf: CGFloat = 28

    var body: some View {
        let locked = controller.inCall
        let amp: CGFloat = reduceMotion ? 0 : 5
        let accent = Mood(PersonaState(controller.phase), dark: scheme == .dark).accent
        let g = dialGeometry()
        VStack(spacing: 2) {
            wheel(locked: locked, amp: amp, accent: accent, g: g)
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
        .onChange(of: open, initial: true) { _, now in
            guard now else { return }
            order = VoiceProvider.allCases.filter { $0 != settings.provider }
            rawTurn = 0
            turn = 0
            lastAngle = nil
        }
        #if DEBUG
        // `REXVOICE_WHEEL_ANGLE=<deg>` (with `REXVOICE_WHEEL=open`): as if the finger grabbed the top logo and turned
        // to that angle (screenshots).
        .task(id: center) {
            guard open, center != .zero,
                  let a = ProcessInfo.processInfo.environment["REXVOICE_WHEEL_ANGLE"].flatMap(Double.init) else { return }
            rawTurn = 0 // the center can settle in more than one pass
            lastAngle = Self.slots[0]
            track(angle: a, g: dialGeometry())
        }
        #endif
    }

    private func wheel(locked: Bool, amp: CGFloat, accent: Color, g: DialGeometry) -> some View {
        Color.clear
            .frame(width: cell, height: cell)
            .contentShape(.circle)
            .onGeometryChange(for: CGPoint.self) { p in
                let f = p.frame(in: .global)
                return CGPoint(x: f.midX, y: f.midY)
            } action: { center = $0 }
            .gesture(press(locked: locked))
            .accessibilityElement()
            .accessibilityLabel("当前：\(settings.provider.shortName)")
            .accessibilityHint(locked ? "通话中不能换" : (open ? "已展开，选一家，或再点一下收起" : "长按换供应商"))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { // VoiceOver double-tap opens / closes the dial directly
                if locked { shakes += 1 } else { open.toggle() }
            }
            .overlay { dial(accent: accent, g: g) }
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
            .task(id: hint) {
                guard hint else { return }
                try? await Task.sleep(for: .seconds(1.6))
                if !Task.isCancelled { withAnimation { hint = false } }
            }
            .sensoryFeedback(trigger: shakes) { _, _ in locked ? .error : .impact(weight: .light) }
            .sensoryFeedback(trigger: open) { _, now in now ? .impact(weight: .medium) : nil }
            .sensoryFeedback(trigger: hover) { _, now in now != nil ? .selection : nil }
            .onChange(of: locked) { _, now in if now { close() } }
    }

    /// Band and all four logos in one view turned by `turn` (the logos turn back so they stay upright). The current
    /// one sits at the center; the other three only exist while open, so VoiceOver never lands on a hidden one.
    private func dial(accent: Color, g: DialGeometry) -> some View {
        let side = 2 * (g.radius + Self.band)
        let move: Animation? = reduceMotion ? nil : .spring(response: 0.32, dampingFraction: 0.72)
        return ZStack {
            if open {
                bandView(radius: g.radius)
                    .transition(reduceMotion ? .opacity : .scale(scale: 0.2).combined(with: .opacity))
            }
            ForEach(VoiceProvider.allCases) { p in
                let current = p == settings.provider
                let slot = order.firstIndex(of: p)
                if current || (open && slot != nil) {
                    let at = current ? .zero : Self.polar(g.radius, Self.slots[slot ?? 0])
                    let on = hover == p
                    Button { pick(p) } label: {
                        ProviderLogo(provider: p, size: Self.logo)
                            .frame(width: cell, height: cell)
                            .overlay { if current && open { Circle().strokeBorder(accent, lineWidth: 2).transition(.opacity) } }
                    }
                    .buttonStyle(PressScale())
                    .allowsHitTesting(!current) // touches on the center go to the hold gesture underneath
                    .accessibilityHidden(current)
                    .accessibilityLabel(p.shortName)
                    .accessibilityHint("选用这家")
                    .scaleEffect(on ? 1.5 : 1)
                    .shadow(color: .black.opacity(on ? (scheme == .dark ? 0.5 : 0.22) : 0), radius: on ? 12 : 0, y: on ? 5 : 0)
                    .rotationEffect(.degrees(turn))
                    .offset(at)
                    .zIndex(on ? 2 : current ? 1 : 0)
                    .transition(reduceMotion ? .opacity : .offset(x: -at.width, y: -at.height)
                        .combined(with: .scale(scale: 0.4)).combined(with: .opacity))
                }
            }
        }
        .frame(width: side, height: side)
        .rotationEffect(.degrees(-turn))
        .animation(move, value: open)
        .animation(reduceMotion ? nil : .interactiveSpring(response: 0.2, dampingFraction: 0.86), value: turn)
        .animation(reduceMotion ? nil : .spring(response: 0.22, dampingFraction: 0.7), value: hover)
    }

    /// The frosted arc band under the three slots, round-ended, with a crisp line along its outer edge.
    private func bandView(radius: CGFloat) -> some View {
        let dark = scheme == .dark
        let (from, to) = (Self.slots.first!, Self.slots.last!)
        let body = ArcBand(radius: radius, from: from, to: to, width: Self.band)
        return body.fill(.regularMaterial)
            .overlay(body.stroke(.white.opacity(dark ? 0.12 : 0.6), lineWidth: 1))
            .overlay(ArcLine(radius: radius + Self.band / 2, from: from, to: to)
                .stroke(Color.primary.opacity(dark ? 0.35 : 0.22), style: StrokeStyle(lineWidth: 1.5, lineCap: .round)))
            .shadow(color: .black.opacity(dark ? 0.4 : 0.14), radius: 18, y: 8)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    /// One gesture for hold / turn / let go, so a tap and a hold never fight over the same touch.
    private func press(locked: Bool) -> some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { v in
                if touchStart == nil {
                    touchStart = v.time
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
                if open { follow(v.location) }
            }
            .onEnded { v in
                holdTimer?.cancel()
                let held = touchStart.map { v.time.timeIntervalSince($0) } ?? 0
                let moved = hypot(v.translation.width, v.translation.height)
                touchStart = nil
                if open {
                    follow(v.location)
                    if let p = hover { pick(p) } else { close() } // nothing under the finger, or a tap on an open dial
                } else if held < 0.35, moved <= 12 {
                    shakes += 1
                    if !locked { withAnimation { hint = true } }
                }
            }
    }

    /// Finger at `location` (cell coordinates): turn the band and update the logo under it.
    private func follow(_ location: CGPoint) {
        let dx = location.x - cell / 2, dy = location.y - cell / 2
        guard hypot(dx, dy) > Self.deadZone else {
            lastAngle = nil
            hover = nil
            return
        }
        track(angle: atan2(-dy, dx) * 180 / .pi, g: dialGeometry())
    }

    private func track(angle: Double, g: DialGeometry) {
        if let last = lastAngle {
            // Past the ends the band only gives a little, so cap how far ahead the finger can wind it.
            rawTurn = min(max(rawTurn + Self.wrap(angle - last), g.lo - 60), g.hi + 60)
        }
        lastAngle = angle
        turn = rubber(rawTurn, g)
        let near = order.indices.map { ($0, abs(Self.wrap(angle - (Self.slots[$0] + turn)))) }.min { $0.1 < $1.1 }
        hover = near.flatMap { $0.1 <= Self.pickTolerance ? order[$0.0] : nil }
    }

    /// Clamp to [lo, hi]; past them the band follows by up to 8° more, easing out (none under reduce motion).
    private func rubber(_ x: Double, _ g: DialGeometry) -> Double {
        let give = reduceMotion ? 0.0 : 8.0
        if x > g.hi { return g.hi + give * (1 - exp(-(x - g.hi) / 20)) }
        if x < g.lo { return g.lo - give * (1 - exp(-(g.lo - x) / 20)) }
        return x
    }

    private func pick(_ p: VoiceProvider) {
        let old = settings.provider
        if let i = order.firstIndex(of: p) { order[i] = old }
        hover = nil
        withAnimation(reduceMotion ? nil : .spring(response: 0.3, dampingFraction: 0.8)) { settings.provider = p }
        picks += 1
        Task {
            if !reduceMotion { try? await Task.sleep(for: .seconds(0.3)) } // let the swap land before closing
            close()
        }
    }

    private func close() {
        open = false
        hover = nil
        lastAngle = nil
    }

    // MARK: Geometry

    struct DialGeometry {
        var radius: CGFloat
        /// Turn range (degrees) that keeps the three slots on screen and clear of the rail.
        var lo: Double
        var hi: Double
    }

    /// The largest radius in 90...110 whose slots fit, and how far the band may turn either way. Every slot center
    /// keeps half a band (+4pt) from the edges of `room`, and a slot below the center logo stays left of the rail.
    private func dialGeometry() -> DialGeometry {
        let m = Self.band / 2 + 4
        func fits(_ r: CGFloat, _ t: Double) -> Bool {
            Self.slots.allSatisfy { s in
                let o = Self.polar(r, s + t)
                let x = center.x + o.width, y = center.y + o.height
                guard x >= room.minX + m, x <= room.maxX - m, y >= room.minY + m, y <= room.maxY - m else { return false }
                return o.height <= cell / 2 || o.width <= -(Self.railHalf + m)
            }
        }
        let r = stride(from: CGFloat(110), through: 90, by: -1).first { fits($0, 0) } ?? 90
        guard fits(r, 0) else { return DialGeometry(radius: r, lo: 0, hi: 0) }
        var hi = 0.0, lo = 0.0
        while hi < 180, fits(r, hi + 1) { hi += 1 }
        while lo > -180, fits(r, lo - 1) { lo -= 1 }
        return DialGeometry(radius: r, lo: lo, hi: hi)
    }

    /// Offset of a point `r` from the center at math angle `deg` (screen y points down).
    static func polar(_ r: CGFloat, _ deg: Double) -> CGSize {
        let a = deg * .pi / 180
        return CGSize(width: r * cos(a), height: -r * sin(a))
    }

    /// Angle difference folded into -180...180.
    static func wrap(_ d: Double) -> Double {
        var x = d.truncatingRemainder(dividingBy: 360)
        if x > 180 { x -= 360 } else if x < -180 { x += 360 }
        return x
    }
}

/// Arc at `radius` around the rect's center from math angle `from` to `to` (counterclockwise on screen).
private struct ArcLine: Shape {
    var radius: CGFloat
    var from: Double
    var to: Double

    func path(in rect: CGRect) -> Path {
        Path { p in
            p.addArc(center: CGPoint(x: rect.midX, y: rect.midY), radius: radius,
                     startAngle: .degrees(-from), endAngle: .degrees(-to), clockwise: true)
        }
    }
}

/// `ArcLine` stroked `width` wide with round ends, as a fillable shape.
private struct ArcBand: Shape {
    var radius: CGFloat
    var from: Double
    var to: Double
    var width: CGFloat

    func path(in rect: CGRect) -> Path {
        ArcLine(radius: radius, from: from, to: to).path(in: rect)
            .strokedPath(StrokeStyle(lineWidth: width, lineCap: .round))
    }
}
