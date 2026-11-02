import AVFoundation
import SwiftUI

/// Main screen. Narrow screens (design J, final 2026-11-02): idle is the big persona card (lit edge, inner glow,
/// grain, state-colored light), a status line and the call button; in a call the card shrinks to a banner over the
/// dialog. The Duo inner screen gets J too (at most 640pt wide, centered); only landscape iPhones (compact height) keep
/// design H's two columns. A rail on the right holds the current provider's logo (hold it for the wheel) plus pairing /
/// settings; wherever the status bar is a column down the right edge (Duo, both screens) the rail is centered in it.
struct ContentView: View {
    @Environment(CallController.self) private var controller
    @Environment(AppSettings.self) private var settings
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.verticalSizeClass) private var vSize
    @State private var sheet: Sheet?
    @State private var callStart: Date?
    /// Provider whose missing key is being asked about before a call.
    @State private var missingKey: VoiceProvider?
    /// Image attachment shown full screen.
    @State private var zoomed: CallController.Attachment?
    /// Natural height of status + transcript + attachments; their scroll view grows to it and no further.
    @State private var infoHeight: CGFloat = .infinity
    /// That part is taller than its room and scrolls; only then is it clipped (card shadows cut at its edges).
    @State private var infoScrolls = false
    /// Provider wheel fanned out; taps elsewhere on the content close it.
    @State private var wheelOpen = false

    enum Sheet: String, Identifiable {
        case settings, pair
        var id: String { rawValue }
    }

    init(initialSheet: Sheet? = nil) {
        _sheet = State(initialValue: initialSheet)
        #if DEBUG
        // `REXVOICE_WHEEL=open`: start with the provider wheel fanned out (screenshots).
        _wheelOpen = State(initialValue: ProcessInfo.processInfo.environment["REXVOICE_WHEEL"] == "open")
        #endif
    }

    var body: some View {
        let phase = controller.phase
        let state = PersonaState(phase)
        let mood = Mood(state, dark: scheme == .dark)
        // Laid out from the live size every pass, so Duo fold / unfold and rotation re-flow it.
        GeometryReader { geo in
            let wide = vSize == .compact
            // Duo: the status bar is a column down the right edge with only the clock / Wi-Fi / battery at its top; the
            // rail is centered in that inset so it lines up under them. Landscape iPhones also have side insets (the
            // Dynamic Island), so compact-height screens never count.
            let column = !wide && geo.safeAreaInsets.trailing >= 56
            let insets = geo.safeAreaInsets
            // Where the provider dial may draw: the whole window width, between the status bar and the home indicator.
            let room = CGRect(x: 0, y: insets.top, width: geo.size.width + insets.leading + insets.trailing,
                              height: geo.size.height)
            // No status bar above us (Duo outer screen, landscape): the top keeps its own margin from the edge.
            let topGap = max(0, 14 - geo.safeAreaInsets.top)
            ZStack {
                HStack(spacing: 12) {
                    Group {
                        if wide {
                            twoColumns(phase, state: state, mood: mood)
                        } else {
                            dialogJ(phase, state: state, mood: mood).frame(maxWidth: 640)
                        }
                    }
                    .overlay {
                        if wheelOpen { Color.clear.contentShape(.rect).onTapGesture { wheelOpen = false } }
                    }
                    if !column { sideRail(room).frame(maxHeight: .infinity, alignment: .bottom) }
                }
                .padding(.top, topGap)
                .padding(.leading, 16)
                .padding(.trailing, column ? 16 : 10)
                .padding(.bottom, 8)
                .frame(maxWidth: 960)
                .frame(maxWidth: .infinity)

                if column {
                    // Bottom-aligned, level with the call button, so it stays clear of the status icons (and whatever
                    // the system grows below them) at the column's top.
                    sideRail(room)
                        .padding(.bottom, 8)
                        .frame(width: geo.safeAreaInsets.trailing)
                        // The status icons sit ~6pt left of the inset's center (measured on the Duo outer screen:
                        // Wi-Fi at x≈418, inset center 424); shift so the logo lines up under them. Inner screen unverified.
                        .offset(x: -6)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                        .ignoresSafeArea(.container, edges: [.top, .trailing])
                }
            }
        }
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
        .fullScreenCover(item: $zoomed) { a in
            if case let .image(image, name) = a.kind { ZoomImage(image: image, name: name) }
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

    private func callButton(_ phase: CallPhase) -> some View {
        V2CallButton(phase: phase, enabled: controller.primaryEnabled, action: primaryAction)
            .padding(.horizontal, 8)
    }

    /// Landscape iPhones (compact height): the card keeps its portrait 0.9:1 on the left, the rest stacks on
    /// the right with the call button level with the card's bottom.
    private func twoColumns(_ phase: CallPhase, state: PersonaState, mood: Mood) -> some View {
        GeometryReader { row in
            let cardWidth = min(row.size.height * 0.9, (row.size.width - 20) / 2)
            HStack(spacing: 20) {
                personaCard(phase, state: state, mood: mood)
                    .frame(width: cardWidth, height: cardWidth / 0.9)
                VStack(spacing: 14) {
                    AskCards(accent: mood.accent)
                    infoSection(phase, mood: mood)
                    Spacer(minLength: 0)
                    callButton(phase)
                }
                .frame(height: cardWidth / 0.9)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// Narrow screens (design J): idle is the big card, one status line and the button. In a call the card shrinks to an 88pt banner
    /// and the dialog takes the middle; a hang-up goes back to idle (the dialog is kept until the next call clears it).
    private func dialogJ(_ phase: CallPhase, state: PersonaState, mood: Mood) -> some View {
        VStack(spacing: 14) {
            if phase.inCall {
                callBanner(phase, state: state, mood: mood)
                    .transition(.opacity.combined(with: .move(edge: .top)))
                AskCards(accent: mood.accent)
                dialog(mood).transition(.opacity)
            } else {
                personaCard(phase, state: state, mood: mood)
                    .frame(maxHeight: .infinity)
                    .transition(.opacity)
                AskCards(accent: mood.accent)
                statusRow(phase, mood: mood)
            }
            callButton(phase)
        }
        .animation(reduceMotion ? .easeInOut(duration: 0.3) : .smooth(duration: 0.45), value: phase.inCall)
    }

    /// J's in-call dialog; notices float over its top.
    private func dialog(_ mood: Mood) -> some View {
        DialogList(lines: controller.transcript, attachments: controller.attachments, accent: mood.accent) { zoomed = $0 }
            .frame(maxHeight: .infinity)
            .overlay(alignment: .top) {
                if let notice = controller.notice {
                    NoticePill(text: notice) { controller.notice = nil }.padding(.top, 8)
                }
            }
    }

    /// J's in-call header: the persona in a 56pt disc, state + hint, the timer, in the big card's light.
    private func callBanner(_ phase: CallPhase, state: PersonaState, mood: Mood) -> some View {
        let shape = RoundedRectangle(cornerRadius: 28, style: .continuous)
        let dark = scheme == .dark
        return HStack(spacing: 14) {
            ZStack {
                RadialGradient(colors: [mood.glow.opacity(0.9), mood.bottom], center: .center, startRadius: 2, endRadius: 34)
                PersonaView(persona: settings.persona, state: state).padding(.top, 8)
            }
            .frame(width: 56, height: 56)
            .clipShape(.circle)
            .overlay(Circle().strokeBorder(.white.opacity(dark ? 0.25 : 0.8), lineWidth: 1))
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(phase.label).font(.headline).contentTransition(.opacity)
                    if phase.live { WaveBars(live: true, count: 4, height: 14).foregroundStyle(mood.accent) }
                }
                Text(phase.hint).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
            TimelineView(.periodic(from: .now, by: 1)) { ctx in
                Text(elapsed(since: callStart, now: ctx.date))
                    .font(.subheadline.weight(.semibold).monospacedDigit())
                    .padding(.horizontal, 10)
                    .frame(minHeight: 28)
                    .background(Capsule().fill(chipFill))
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 88)
        .background {
            LinearGradient(colors: [mood.top, mood.bottom], startPoint: .leading, endPoint: .trailing)
            GrainOverlay(opacity: 0.1)
        }
        .clipShape(shape)
        .overlay(shape.strokeBorder(LinearGradient(colors: [.white.opacity(dark ? 0.35 : 0.9), .white.opacity(dark ? 0.04 : 0.2)],
                                                   startPoint: .top, endPoint: .bottom), lineWidth: 1))
        .shadow(color: mood.glow.opacity(dark ? 0.25 : 0.35), radius: 20, y: 8)
        .accessibilityElement(children: .combine)
    }

    /// Status, transcript and attachments, scrolling only when they do not fit. Unclipped while they fit so the card
    /// shadows draw in full; while scrolling the 12pt / 4pt inset keeps most of them.
    private func infoSection(_ phase: CallPhase, mood: Mood) -> some View {
        ScrollView {
            VStack(spacing: 14) {
                statusRow(phase, mood: mood)
                transcriptCard(phase, mood: mood)
                attachmentsCard(mood)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 4)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { infoHeight = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: Bool.self) { $0.contentSize.height > $0.containerSize.height + 0.5 }
                                action: { _, scrolls in infoScrolls = scrolls }
        .scrollClipDisabled(!infoScrolls)
        .frame(maxHeight: infoHeight)
        .padding(.horizontal, -12)
        .padding(.vertical, -4)
    }

    /// 56pt rail, bottom-aligned: the current provider's logo and name (hold for the wheel), then pairing and settings.
    private func sideRail(_ room: CGRect) -> some View {
        VStack(spacing: 8) {
            ProviderWheel(open: $wheelOpen, cell: 48, room: room)
            V2IconButton(title: "配对", symbol: "qrcode.viewfinder") { sheet = .pair }
            V2IconButton(title: "设置", symbol: "slider.horizontal.3") { sheet = .settings }
        }
        .padding(.vertical, 6)
        .frame(width: 56)
        .background(CardSurface(radius: 28).opacity(0.92))
    }

    private var pageColor: Color { scheme == .dark ? Color(white: 0.04) : Color(white: 0.95) }

    private var pageBackground: some View {
        ZStack {
            pageColor
            GrainOverlay(opacity: 0.05)
        }
        .ignoresSafeArea()
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
            // The provider shows as the highlighted logo in the rail, so only the unpaired line stays.
            if controller.link == .unpaired {
                Text("还没配对 Mac").font(.footnote).foregroundStyle(.secondary)
            }
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
                    .padding(.top, 6)
            }
        }
        .padding(.horizontal, 6)
        .fixedSize(horizontal: false, vertical: true) // never squeezed by the card's layout priority
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

    /// Images (tap for full screen) and links (open in Safari) from the Mac, newest first; hidden when there are none.
    @ViewBuilder private func attachmentsCard(_ mood: Mood) -> some View {
        let items = controller.attachments.reversed()
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text("附件").font(.caption.weight(.bold)).foregroundStyle(.secondary)
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(items) { a in
                            if case let .image(image, name) = a.kind {
                                Button { zoomed = a } label: {
                                    Image(uiImage: image).resizable().scaledToFit().frame(height: 72)
                                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                                }
                                .buttonStyle(PressScale())
                                .accessibilityLabel("图片 \(name)")
                            }
                        }
                    }
                }
                .scrollClipDisabled()
                ForEach(items) { a in
                    if case let .link(url, title) = a.kind {
                        Link(destination: url) {
                            Label { Text(title).lineLimit(1) } icon: { Image(systemName: "link").foregroundStyle(mood.accent) }
                                .font(.subheadline)
                                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                        }
                        .foregroundStyle(.primary)
                    }
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(CardSurface(radius: 24).opacity(0.92))
            .fixedSize(horizontal: false, vertical: true)
            .transition(.opacity.combined(with: .move(edge: .bottom)))
        }
    }
}

/// Full-screen image: pinch to zoom (1x-6x), drag to pan while zoomed, double-tap toggles 2.5x.
private struct ZoomImage: View {
    let image: UIImage
    let name: String
    @Environment(\.dismiss) private var dismiss
    @State private var scale: CGFloat = 1
    @State private var offset = CGSize.zero
    @GestureState private var pinch: CGFloat = 1
    @GestureState private var drag = CGSize.zero

    var body: some View {
        Image(uiImage: image).resizable().scaledToFit()
            .accessibilityLabel("图片 \(name)")
            .scaleEffect(scale * pinch)
            .offset(x: offset.width + drag.width, y: offset.height + drag.height)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .contentShape(.rect)
            .gesture(MagnifyGesture()
                .updating($pinch) { v, s, _ in s = v.magnification }
                .onEnded { v in
                    scale = min(max(scale * v.magnification, 1), 6)
                    if scale == 1 { offset = .zero }
                }
                .simultaneously(with: DragGesture()
                    .updating($drag) { v, s, _ in if scale > 1 { s = v.translation } }
                    .onEnded { v in
                        guard scale > 1 else { return }
                        offset.width += v.translation.width
                        offset.height += v.translation.height
                    }))
            .onTapGesture(count: 2) {
                withAnimation(.smooth) {
                    scale = scale > 1 ? 1 : 2.5
                    offset = .zero
                }
            }
            .background(Color.black.ignoresSafeArea())
            .overlay(alignment: .topTrailing) {
                V2IconButton(title: "关闭", symbol: "xmark") { dismiss() }.padding(16)
            }
            .environment(\.colorScheme, .dark)
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
