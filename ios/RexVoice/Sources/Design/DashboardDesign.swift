import SwiftUI

/// Draft C「仪表」: a big glass status card (Mac / state / duration) on top, a plain transcript log in
/// the middle, a glass toolbar (pair / call / settings) at the bottom.
struct DashboardDesign: View {
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Namespace private var glass
    @State private var sheet: DesignSheet?
    @State private var callStart: Date?

    init(initialSheet: DesignSheet? = nil) { _sheet = State(initialValue: initialSheet) }

    var body: some View {
        let phase = controller.phase
        VStack(spacing: 16) {
            statusCard(phase)
            log(phase)
        }
        .padding(.horizontal)
        .background { backdrop(phase) }
        .safeAreaBar(edge: .bottom) { toolbar(phase) }
        .animation(.smooth, value: phase)
        .animation(.smooth, value: controller.notice)
        .onChange(of: controller.inCall, initial: true) { _, on in callStart = on ? .now : nil }
        .sheet(item: $sheet) { s in
            switch s {
            case .settings: DesignSettings(tint: .teal) { sheet = .pair }
            case .pair: DesignPairPage(tint: .teal)
            }
        }
        .autoHideNotice(controller)
    }

    private func backdrop(_ phase: CallPhase) -> some View {
        ZStack(alignment: .top) {
            Color(.systemGroupedBackground)
            RadialGradient(colors: [phase.color.opacity(scheme == .dark ? 0.55 : 0.4), .clear],
                           center: .topLeading, startRadius: 20, endRadius: 520)
        }
        .ignoresSafeArea()
    }

    private func statusCard(_ phase: CallPhase) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 10) {
                Image(systemName: "desktopcomputer")
                    .font(.title3)
                    .frame(width: 40, height: 40)
                    .glassEffect(.regular, in: .circle)
                VStack(alignment: .leading, spacing: 2) {
                    Text(phase == .unpaired ? "还没配对 Mac" : DesignSample.macName).font(.headline)
                    Text(phase == .offline ? "离线" : (phase == .unpaired ? "—" : "经 r.maruiao.top 中继"))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Circle().fill(phase.color).frame(width: 10, height: 10).accessibilityHidden(true)
            }
            HStack(alignment: .lastTextBaseline) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(phase.label)
                        .font(.system(size: 44, weight: .bold, design: .rounded))
                        .contentTransition(.opacity)
                    Text(phase.hint).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer()
                if phase.inCall {
                    VStack(alignment: .trailing, spacing: 6) {
                        WaveBars(live: phase.live, count: 7, height: 26).foregroundStyle(phase.color)
                        TimelineView(.periodic(from: .now, by: 1)) { ctx in
                            Text(elapsed(since: callStart, now: ctx.date))
                                .font(.title3.weight(.semibold)).monospacedDigit()
                        }
                    }
                }
            }
            HStack(spacing: 8) {
                chip("链路", controller.link == .ready ? "在线" : phase.label, ok: controller.link == .ready)
                chip("通话", phase.inCall ? phase.label : "未开始", ok: phase.inCall)
                chip("门限", String(format: "%.3f", controller.threshold), ok: true)
            }
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
            }
        }
        .padding(20)
        .glassEffect(.regular.tint(phase.color.opacity(0.18)), in: .rect(cornerRadius: 32))
        .padding(.top, 8)
    }

    private func chip(_ title: String, _ value: String, ok: Bool) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title).font(.caption2).foregroundStyle(.secondary)
            Text(value).font(.footnote.weight(.semibold)).foregroundStyle(ok ? .primary : .secondary).lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.background.opacity(0.35), in: .rect(cornerRadius: 12))
    }

    private func log(_ phase: CallPhase) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                ForEach(controller.transcript) { line in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(line.fromUser ? "我" : "Rex")
                            .font(.caption.weight(.bold))
                            .foregroundStyle(line.fromUser ? AnyShapeStyle(.secondary) : AnyShapeStyle(phase.color))
                            .frame(width: 30, alignment: .leading)
                        Text(line.text).font(.body)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 4)
        }
        .defaultScrollAnchor(.bottom)
        .scrollEdgeEffectHidden(true, for: .top)
        .overlay {
            if controller.transcript.isEmpty { EmptyHint(phase: phase) }
        }
    }

    private func toolbar(_ phase: CallPhase) -> some View {
        GlassEffectContainer(spacing: 14) {
            HStack(spacing: 14) {
                Button("配对", systemImage: "qrcode.viewfinder") { sheet = .pair }
                    .labelStyle(.iconOnly)
                    .font(.title2)
                    .frame(width: 60, height: 60)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .glassEffectID("pair", in: glass)
                Button {
                    primaryAction(controller) { sheet = .pair }
                } label: {
                    Label(phase.inCall ? "挂断" : (phase == .unpaired ? "配对" : "开始通话"),
                          systemImage: phase.inCall ? "phone.down.fill" : (phase == .unpaired ? "qrcode.viewfinder" : "phone.fill"))
                        .font(.headline)
                        .foregroundStyle(controller.primaryEnabled ? AnyShapeStyle(.white) : AnyShapeStyle(.tertiary))
                        .frame(maxWidth: .infinity, minHeight: 60)
                }
                // Glass inside a container ignores .opacity, so disabled shows as untinted glass instead.
                .glassEffect(.regular.tint(controller.primaryEnabled ? (phase.inCall ? .red : (phase == .unpaired ? .indigo : .green)) : nil)
                    .interactive(), in: .capsule)
                .glassEffectID("call", in: glass)
                .disabled(!controller.primaryEnabled)
                Button("设置", systemImage: "gearshape") { sheet = .settings }
                    .labelStyle(.iconOnly)
                    .font(.title2)
                    .frame(width: 60, height: 60)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .glassEffectID("settings", in: glass)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal)
        .padding(.bottom, 4)
    }
}
