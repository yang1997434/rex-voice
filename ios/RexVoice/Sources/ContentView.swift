import AVFoundation
import SwiftUI

/// Main screen (design E「卡片」): full-screen dark, the persona in one thick card (lit edge, inner glow, grain,
/// state-colored light), status and transcript in cards below it; settings is a page of the same cards.
struct ContentView: View {
    @Environment(CallController.self) private var controller
    @Environment(AppSettings.self) private var settings
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var sheet: Sheet?
    @State private var callStart: Date?
    /// Provider whose missing key is being asked about before a call.
    @State private var missingKey: VoiceProvider?

    enum Sheet: String, Identifiable {
        case settings, pair
        var id: String { rawValue }
    }

    init(initialSheet: Sheet? = nil) { _sheet = State(initialValue: initialSheet) }

    var body: some View {
        let phase = controller.phase
        let state = PersonaState(phase)
        let mood = Mood(state, dark: scheme == .dark)
        VStack(spacing: 14) {
            topBar
            personaCard(phase, state: state, mood: mood)
            statusRow(phase, mood: mood)
            transcriptCard(phase, mood: mood)
            Spacer(minLength: 0)
            V2CallButton(phase: phase, enabled: controller.primaryEnabled, action: primaryAction)
            .padding(.horizontal, 8)
            .padding(.bottom, 8)
        }
        .padding(.horizontal, 16)
        .background { pageBackground }
        .animation(reduceMotion ? .easeInOut(duration: 0.3) : .smooth(duration: 0.5), value: state)
        .animation(.smooth, value: controller.notice)
        .onChange(of: phase.inCall, initial: true) { _, inCall in callStart = inCall ? Date() : nil }
        .fullScreenCover(item: $sheet) { s in
            switch s {
            case .settings: SettingsPage { sheet = .pair }
            case .pair: PairPage()
            }
        }
        .alert("还没填 \(missingKey?.title ?? "") 的 API key", isPresented: Binding(get: { missingKey != nil },
                                                                              set: { if !$0 { missingKey = nil } })) {
            Button("去填") { sheet = .settings }
            Button("先用 Mac 上的") { Task { await controller.toggleCall() } }
        } message: {
            Text("Mac 钥匙串里有这家的 key 就能直接打；没有的话通话会报错。这个提示只出现一次。")
        }
        .task(id: controller.notice) {
            guard controller.notice != nil else { return }
            try? await Task.sleep(for: .seconds(5))
            if !Task.isCancelled { controller.notice = nil }
        }
    }

    private func primaryAction() {
        if controller.link == .unpaired, !controller.inCall {
            sheet = .pair
        } else if !controller.inCall, settings.shouldPromptForKey(settings.provider) {
            settings.markKeyPrompted(settings.provider)
            missingKey = settings.provider
        } else {
            Task { await controller.toggleCall() }
        }
    }

    private var pageBackground: some View {
        ZStack {
            scheme == .dark ? Color(white: 0.04) : Color(white: 0.95)
            GrainOverlay(opacity: 0.05)
        }
        .ignoresSafeArea()
    }

    private var topBar: some View {
        HStack(spacing: 10) {
            Wordmark()
            Spacer()
            V2IconButton(title: "配对", symbol: "qrcode.viewfinder") { sheet = .pair }
            V2IconButton(title: "设置", symbol: "slider.horizontal.3") { sheet = .settings }
        }
        .padding(.horizontal, 4)
        .padding(.top, 4)
    }

    /// The thick card: state-colored light inside, grain, a lit top edge, inner glow, two-layer shadow.
    private func personaCard(_ phase: CallPhase, state: PersonaState, mood: Mood) -> some View {
        let shape = RoundedRectangle(cornerRadius: 34, style: .continuous)
        let dark = scheme == .dark
        return ZStack(alignment: .bottom) {
            LinearGradient(colors: [mood.top, mood.bottom], startPoint: .top, endPoint: .bottom)
            RadialGradient(colors: [mood.glow.opacity(dark ? 0.7 : 0.8), .clear], center: UnitPoint(x: 0.5, y: 0.4),
                           startRadius: 10, endRadius: 230)
            GrainOverlay(opacity: 0.1)
            VoiceHalo(active: phase == .listening, level: Double(controller.level), color: mood.accent)
                .padding(.horizontal, 30)
                .padding(.bottom, 20)
            PersonaView(persona: settings.persona, state: state)
                .padding(.top, 34)
                .saturation(state == .offline ? 0.35 : 1)
                .opacity(state == .offline ? 0.8 : 1)
            // specular sheen across the top third
            LinearGradient(colors: [.white.opacity(dark ? 0.1 : 0.3), .clear], startPoint: .top, endPoint: UnitPoint(x: 0.5, y: 0.35))
                .allowsHitTesting(false)
        }
        .overlay(alignment: .topLeading) { stateChip(phase, mood: mood).padding(14) }
        .overlay(alignment: .topTrailing) {
            if phase.inCall {
                TimelineView(.periodic(from: .now, by: 1)) { ctx in
                    Text(elapsed(since: callStart, now: ctx.date))
                        .font(.footnote.weight(.semibold).monospacedDigit())
                        .padding(.horizontal, 10)
                        .frame(minHeight: 28)
                        .background(Capsule().fill(chipFill))
                        .foregroundStyle(chipText)
                }
                .padding(14)
            }
        }
        .clipShape(shape)
        .overlay(shape.stroke(.white.opacity(dark ? 0.16 : 0.5), lineWidth: 10).blur(radius: 8).clipShape(shape))
        .overlay(shape.strokeBorder(LinearGradient(colors: [.white.opacity(dark ? 0.35 : 0.9), .white.opacity(dark ? 0.04 : 0.2)],
                                                   startPoint: .top, endPoint: .bottom), lineWidth: 1))
        .shadow(color: mood.glow.opacity(dark ? 0.25 : 0.35), radius: 30, y: 14)
        .shadow(color: .black.opacity(dark ? 0.5 : 0.1), radius: 2, y: 1)
        .containerRelativeFrame(.vertical) { h, _ in h * 0.53 }
    }

    // chips sit on the card's light: dark smoke in dark mode, milky white in light mode
    private var chipFill: Color { scheme == .dark ? .black.opacity(0.28) : .white.opacity(0.7) }
    private var chipText: Color { scheme == .dark ? .white : .primary }

    private func stateChip(_ phase: CallPhase, mood: Mood) -> some View {
        HStack(spacing: 6) {
            Circle().fill(mood.accent).frame(width: 7, height: 7)
            Text(phase.label).font(.footnote.weight(.semibold))
        }
        .padding(.horizontal, 10)
        .frame(minHeight: 28)
        .background(Capsule().fill(chipFill))
        .foregroundStyle(chipText)
        .contentTransition(.opacity)
    }

    private func statusRow(_ phase: CallPhase, mood: Mood) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(phase.hint).font(.headline)
                Spacer()
                if phase.live { WaveBars(live: true, count: 4, height: 14).foregroundStyle(mood.accent) }
            }
            Text(controller.link == .unpaired ? "还没配对 Mac"
                 : settings.provider.title + (controller.relayHost.map { " · 经 \($0)" } ?? ""))
                .font(.footnote)
                .foregroundStyle(.secondary)
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
                    .padding(.top, 6)
            }
        }
        .padding(.horizontal, 6)
    }

    private func transcriptCard(_ phase: CallPhase, mood: Mood) -> some View {
        let lines = Array(controller.transcript.suffix(2))
        return VStack(alignment: .leading, spacing: 10) {
            if lines.isEmpty {
                EmptyHint(phase: phase).frame(maxWidth: .infinity, minHeight: 40)
            }
            ForEach(Array(lines.enumerated()), id: \.element.id) { i, line in
                if i > 0 { Divider().opacity(0.6) }
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text(line.fromUser ? "我" : "Rex")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(line.fromUser ? AnyShapeStyle(.secondary) : AnyShapeStyle(mood.accent))
                        .frame(width: 28, alignment: .leading)
                    Text(line.text)
                        .font(.subheadline)
                        .lineLimit(2)
                        .foregroundStyle(i == lines.count - 1 ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                }
                .accessibilityElement(children: .combine)
                .transition(.opacity.combined(with: .move(edge: .bottom)))
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(CardSurface(radius: 24).opacity(0.92))
    }
}

/// Asks for the camera first: DataScannerViewController.isAvailable stays false until access is granted.
struct ScannerSheet: View {
    let onCode: (String) -> Void
    let onError: (String) -> Void
    @State private var granted: Bool?

    var body: some View {
        Group {
            switch granted {
            case nil:
                ProgressView()
            case true? where PairScannerView.isUsable:
                PairScannerView(onCode: onCode, onError: onError).ignoresSafeArea()
            default:
                ContentUnavailableView("没法扫码", systemImage: "camera.badge.ellipsis",
                                       description: Text("这台设备不支持扫码，或没给相机权限（设置 > Rex Voice）。"))
            }
        }
        .task { granted = await AVCaptureDevice.requestAccess(for: .video) }
    }
}
