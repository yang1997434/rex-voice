import SwiftUI

/// Draft A「电话」: full-screen gradient that follows the call phase, one big round glass button with
/// ripples behind it, the latest lines rising from the bottom as glass cards.
struct PhoneDesign: View {
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var sheet: DesignSheet?

    init(initialSheet: DesignSheet? = nil) { _sheet = State(initialValue: initialSheet) }

    var body: some View {
        let phase = controller.phase
        VStack(spacing: 0) {
            topBar
            status(phase)
            Spacer(minLength: 12)
            bigButton(phase)
            Spacer(minLength: 12)
            cards(phase)
        }
        .background { background(phase) }
        .animation(reduceMotion ? nil : .smooth(duration: 0.5), value: phase)
        .animation(.smooth, value: controller.notice)
        .sheet(item: $sheet) { s in
            switch s {
            case .settings: DesignSettings(tint: .blue, tintedBackground: true) { sheet = .pair }
            case .pair: DesignPairPage(tint: .blue)
            }
        }
        .autoHideNotice(controller)
    }

    private func background(_ phase: CallPhase) -> some View {
        ZStack {
            Color(.systemBackground)
            LinearGradient(colors: [phase.color.opacity(scheme == .dark ? 0.75 : 0.55),
                                    phase.color.opacity(scheme == .dark ? 0.25 : 0.15),
                                    phase.color.opacity(scheme == .dark ? 0.45 : 0.3)],
                           startPoint: .top, endPoint: .bottom)
        }
        .ignoresSafeArea()
    }

    private var topBar: some View {
        GlassEffectContainer(spacing: 12) {
            HStack {
                Button("设置", systemImage: "gearshape") { sheet = .settings }
                Spacer()
                Button("配对", systemImage: "qrcode.viewfinder") { sheet = .pair }
            }
            .labelStyle(.iconOnly)
            .font(.title3)
            .buttonStyle(.glass)
            .buttonBorderShape(.circle)
            .controlSize(.large)
        }
        .padding(.horizontal)
    }

    private func status(_ phase: CallPhase) -> some View {
        VStack(spacing: 8) {
            Text(phase.label)
                .font(.system(size: 40, weight: .bold, design: .rounded))
                .contentTransition(.opacity)
            Text(phase.hint)
                .font(.callout)
                .foregroundStyle(.secondary)
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
                    .padding(.top, 6)
            }
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal)
        .padding(.top, 8)
    }

    private func bigButton(_ phase: CallPhase) -> some View {
        let tint: Color = phase.inCall ? .red : (phase == .unpaired ? .indigo : .green)
        let symbol = phase.inCall ? "phone.down.fill" : (phase == .unpaired ? "qrcode.viewfinder" : "phone.fill")
        let title = phase.inCall ? "挂断" : (phase == .unpaired ? "配对" : "开始通话")
        return VStack(spacing: 14) {
            ZStack {
                Ripples(live: phase.live, color: phase.color)
                Button {
                    primaryAction(controller) { sheet = .pair }
                } label: {
                    Image(systemName: symbol)
                        .font(.system(size: 52, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(width: 168, height: 168)
                        .contentTransition(.symbolEffect(.replace))
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(tint.opacity(0.85)).interactive(), in: .circle)
                .disabled(!controller.primaryEnabled)
                .opacity(controller.primaryEnabled ? 1 : 0.5)
                .accessibilityLabel(title)
            }
            .frame(width: 280, height: 280)
            HStack(spacing: 8) {
                if phase.inCall { WaveBars(live: phase.live, height: 16).foregroundStyle(phase.color) }
                Text(title).font(.headline)
            }
        }
    }

    private func cards(_ phase: CallPhase) -> some View {
        let lines = controller.transcript.suffix(3)
        return GlassEffectContainer(spacing: 10) {
            VStack(spacing: 10) {
                if lines.isEmpty {
                    EmptyHint(phase: phase)
                        .frame(maxWidth: .infinity, minHeight: 56)
                        .glassEffect(.clear, in: .rect(cornerRadius: 22))
                }
                ForEach(lines) { line in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(line.fromUser ? "我" : "Rex")
                            .font(.caption.weight(.bold))
                            .foregroundStyle(line.fromUser ? AnyShapeStyle(.secondary) : AnyShapeStyle(phase.color))
                            .frame(width: 30, alignment: .leading)
                        Text(line.text).font(.subheadline)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
                    .glassEffect(.regular, in: .rect(cornerRadius: 22))
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .accessibilityElement(children: .combine)
                }
            }
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
    }
}

/// Rings spreading from the button while audio flows.
private struct Ripples: View {
    var live: Bool
    var color: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(paused: !live || reduceMotion)) { ctx in
            let t = ctx.date.timeIntervalSinceReferenceDate
            ZStack {
                ForEach(0..<3, id: \.self) { i in
                    let p = live && !reduceMotion ? (t / 2.4 + Double(i) / 3).truncatingRemainder(dividingBy: 1) : 0.3 + Double(i) * 0.25
                    Circle()
                        .stroke(color.opacity(live ? 0.6 * (1 - p) : 0.18), lineWidth: 3)
                        .frame(width: 168 + 112 * p, height: 168 + 112 * p)
                }
            }
        }
        .accessibilityHidden(true)
    }
}
