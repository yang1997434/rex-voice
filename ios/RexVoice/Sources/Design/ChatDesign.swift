import SwiftUI

/// Draft B「对话」: an iMessage-like transcript is the screen; glass nav bar, a status capsule on top
/// and a glass call capsule at the bottom.
struct ChatDesign: View {
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @State private var sheet: DesignSheet?
    @State private var callStart: Date?

    init(initialSheet: DesignSheet? = nil) { _sheet = State(initialValue: initialSheet) }

    var body: some View {
        let phase = controller.phase
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: 6) {
                    ForEach(controller.transcript) { line in bubble(line) }
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
            }
            .defaultScrollAnchor(.bottom)
            .scrollEdgeEffectStyle(.soft, for: .all)
            .overlay {
                if controller.transcript.isEmpty {
                    VStack(spacing: 12) {
                        Image(systemName: phase.symbol).font(.system(size: 40)).foregroundStyle(phase.color)
                        EmptyHint(phase: phase)
                    }
                }
            }
            .background { wallpaper }
            .safeAreaBar(edge: .top) { statusCapsule(phase) }
            .safeAreaBar(edge: .bottom) { callBar(phase) }
            .navigationTitle("Rex Voice")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("设置", systemImage: "gearshape") { sheet = .settings }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("配对", systemImage: "qrcode.viewfinder") { sheet = .pair }
                }
            }
        }
        .animation(.smooth, value: phase)
        .animation(.smooth, value: controller.notice)
        .onChange(of: controller.inCall, initial: true) { _, on in callStart = on ? .now : nil }
        .sheet(item: $sheet) { s in
            switch s {
            case .settings: DesignSettings(tint: .blue) { sheet = .pair }
            case .pair: DesignPairPage(tint: .blue)
            }
        }
        .autoHideNotice(controller)
    }

    private var wallpaper: some View {
        LinearGradient(colors: scheme == .dark
                       ? [Color(red: 0.08, green: 0.1, blue: 0.2), Color(red: 0.02, green: 0.02, blue: 0.05)]
                       : [Color(red: 0.86, green: 0.91, blue: 1), Color(red: 0.98, green: 0.96, blue: 0.94)],
                       startPoint: .top, endPoint: .bottom)
            .ignoresSafeArea()
    }

    private func bubble(_ line: CallController.Line) -> some View {
        Text(line.text)
            .foregroundStyle(line.fromUser ? .white : .primary)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .glassEffect(line.fromUser ? .regular.tint(.blue) : .regular, in: .rect(cornerRadius: 20))
            .frame(maxWidth: 300, alignment: line.fromUser ? .trailing : .leading)
            .frame(maxWidth: .infinity, alignment: line.fromUser ? .trailing : .leading)
            .accessibilityLabel((line.fromUser ? "我：" : "助手：") + line.text)
    }

    private func statusCapsule(_ phase: CallPhase) -> some View {
        VStack(spacing: 8) {
            Label {
                Text(phase.label).font(.subheadline.weight(.semibold))
            } icon: {
                Circle().fill(phase.color).frame(width: 9, height: 9)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .glassEffect(.regular.tint(phase.color.opacity(0.25)), in: .capsule)
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
            }
        }
        .padding(.top, 4)
    }

    private func callBar(_ phase: CallPhase) -> some View {
        GlassEffectContainer(spacing: 10) {
            HStack(spacing: 10) {
                if phase.inCall {
                    HStack(spacing: 10) {
                        WaveBars(live: phase.live).foregroundStyle(phase.color)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(phase.hint).font(.subheadline.weight(.medium)).lineLimit(1)
                            TimelineView(.periodic(from: .now, by: 1)) { ctx in
                                Text(elapsed(since: callStart, now: ctx.date))
                                    .font(.caption).monospacedDigit().foregroundStyle(.secondary)
                            }
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 18)
                    .frame(height: 60)
                    .glassEffect(.regular, in: .capsule)
                }
                Button {
                    primaryAction(controller) { sheet = .pair }
                } label: {
                    if phase.inCall {
                        Image(systemName: "phone.down.fill")
                            .font(.title3.weight(.semibold))
                            .frame(width: 44, height: 44)
                            .accessibilityLabel("挂断")
                    } else {
                        Label(phase == .unpaired ? "配对" : "开始通话",
                              systemImage: phase == .unpaired ? "qrcode.viewfinder" : "phone.fill")
                            .font(.headline)
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                }
                .buttonStyle(.glassProminent)
                .buttonBorderShape(phase.inCall ? .circle : .capsule)
                .tint(phase.inCall ? .red : (phase == .unpaired ? .indigo : .green))
                .disabled(!controller.primaryEnabled)
            }
        }
        .padding(.horizontal)
        .padding(.bottom, 4)
    }
}
