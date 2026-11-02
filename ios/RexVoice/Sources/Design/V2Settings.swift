import SwiftUI

/// D draws its settings as a pull-up panel on frosted cards; E as a page of thick cards.
enum V2Look { case room, card }

/// Card surface for the two looks.
struct V2Surface: View {
    let look: V2Look
    var radius: CGFloat = 26
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: radius, style: .continuous)
        let dark = scheme == .dark
        switch look {
        case .room:
            shape.fill(.ultraThinMaterial)
                .overlay(shape.fill(dark ? Color.white.opacity(0.04) : Color.white.opacity(0.35)))
                .overlay(shape.strokeBorder(.white.opacity(dark ? 0.1 : 0.7), lineWidth: 1))
        case .card:
            shape.fill(LinearGradient(colors: dark ? [Color(white: 0.115), Color(white: 0.075)] : [.white, Color(white: 0.975)],
                                      startPoint: .top, endPoint: .bottom))
                // inner glow: a blurred light stroke kept inside the edge
                .overlay(shape.stroke(.white.opacity(dark ? 0.07 : 0.6), lineWidth: 8).blur(radius: 6).clipShape(shape))
                // top-lit edge
                .overlay(shape.strokeBorder(LinearGradient(colors: dark ? [.white.opacity(0.2), .white.opacity(0.03)]
                                                                     : [.white, .black.opacity(0.07)],
                                                           startPoint: .top, endPoint: .bottom), lineWidth: 1))
                .shadow(color: .black.opacity(dark ? 0.45 : 0.08), radius: 18, y: 10)
                .shadow(color: .black.opacity(dark ? 0.3 : 0.05), radius: 1.5, y: 1)
        }
    }
}

/// Hark-style section: small icon chip + "标题 · 说明", then content.
struct V2Section<Content: View>: View {
    let look: V2Look
    let symbol: String
    let title: String
    var caption: String?
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(spacing: 8) {
                Image(systemName: symbol)
                    .font(.footnote.weight(.semibold))
                    .frame(width: 26, height: 26)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(.primary.opacity(0.08)))
                Text(title).font(.subheadline.weight(.semibold))
                if let caption {
                    Text("· \(caption)").font(.subheadline).foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            content
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(V2Surface(look: look))
    }
}

/// Light pill button like Hark's "Pay Now".
struct V2Pill: View {
    let title: String
    var symbol: String?
    var destructive = false
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let symbol { Image(systemName: symbol) }
                Text(title)
            }
            .font(.subheadline.weight(.medium))
            .foregroundStyle(destructive ? Color.red : .primary)
            .padding(.horizontal, 16)
            .frame(minHeight: 40)
            .background(Capsule().fill(scheme == .dark ? Color.white.opacity(0.1) : Color.black.opacity(0.05)))
            .overlay(Capsule().strokeBorder(.white.opacity(scheme == .dark ? 0.1 : 0.8), lineWidth: 1))
        }
        .buttonStyle(PressScale())
    }
}

struct V2Settings: View {
    let look: V2Look
    var onRepair: () -> Void

    @Environment(CallController.self) private var controller
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @AppStorage(PersonaStyle.storageKey) private var persona = PersonaStyle.pixel
    @State private var provider = VoiceProvider.openai
    @State private var model = VoiceProvider.openai.models[0]
    @State private var voice = VoiceProvider.openai.voices[0].value
    @State private var keySaved = true
    @State private var keyDraft = ""

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(spacing: 14) {
                    header
                    personaSection.id("persona")
                    voiceSection.id("voices")
                    keySection
                    micSection
                    pairSection
                    aboutSection.id("bottom")
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 40)
            }
            .scrollIndicators(.hidden)
            #if DEBUG
            .task {
                // `REXVOICE_PROVIDER=grok` picks the provider; `REXVOICE_SCROLL=voices|custom|bottom` lands a screenshot there.
                if let raw = ProcessInfo.processInfo.environment["REXVOICE_PROVIDER"], let p = VoiceProvider(rawValue: raw) {
                    provider = p
                    model = p.models[0]
                    voice = p.voices[0].value
                }
                guard let anchor = ProcessInfo.processInfo.environment["REXVOICE_SCROLL"] else { return }
                try? await Task.sleep(for: .milliseconds(400))
                proxy.scrollTo(anchor, anchor: anchor == "custom" ? .bottom : .top)
            }
            #endif
        }
        .background { if look == .card { cardPageBackground } }
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            if look == .card {
                V2IconButton(title: "返回", symbol: "chevron.left") { dismiss() }
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 6 }
            }
            Text("设置").font(.system(.largeTitle, design: .serif).weight(.semibold))
            Spacer()
            if look == .room {
                V2Pill(title: "完成") { dismiss() }
            }
        }
        .padding(.top, look == .room ? 24 : 8)
        .padding(.bottom, 4)
    }

    private var cardPageBackground: some View {
        ZStack {
            (scheme == .dark ? Color(white: 0.04) : Color(white: 0.95))
            RadialGradient(colors: [Color(red: 0.94, green: 0.63, blue: 0.49).opacity(scheme == .dark ? 0.18 : 0.22), .clear],
                           center: .top, startRadius: 0, endRadius: 420)
            GrainOverlay(opacity: 0.05)
        }
        .ignoresSafeArea()
    }

    // MARK: 形象

    private var personaSection: some View {
        V2Section(look: look, symbol: "person.crop.square", title: "形象", caption: "通话时她会跟着换表情") {
            ScrollViewReader { carousel in
            ScrollView(.horizontal) {
                LazyHStack(spacing: 12) {
                    ForEach(PersonaStyle.allCases) { style in
                        // E puts a bigger hero carousel on top of its page; D keeps it compact inside the panel
                        PersonaCard(style: style, selected: style == persona, size: look == .card ? CGSize(width: 200, height: 240)
                                                                                                    : CGSize(width: 148, height: 172)) {
                            if style.available { persona = style }
                        }
                        .id(style.id)
                    }
                }
                .scrollTargetLayout()
                .padding(.vertical, 6)
            }
            .scrollTargetBehavior(.viewAligned)
            .contentMargins(.horizontal, 18, for: .scrollContent)
            .scrollIndicators(.hidden)
            .padding(.horizontal, -18)
            .onAppear { carousel.scrollTo(persona.id, anchor: .center) } // open on the current pick
            }
        }
    }

    // MARK: 声音

    private var voiceSection: some View {
        V2Section(look: look, symbol: "waveform", title: "声音", caption: provider.title) {
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(VoiceProvider.allCases) { p in
                    Button {
                        provider = p
                        model = p.models[0]
                        voice = p.voices[0].value
                    } label: {
                        HStack(spacing: 8) {
                            BrandMark(provider: p)
                            Text(p.title).font(.subheadline.weight(.medium)).lineLimit(1)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, 10)
                        .frame(minHeight: 44)
                        .background(Capsule().fill(provider == p ? Color.primary.opacity(scheme == .dark ? 0.14 : 0.07) : .clear))
                        .overlay(Capsule().strokeBorder(provider == p ? Color.primary.opacity(0.55) : Color.primary.opacity(0.12),
                                                        lineWidth: provider == p ? 1.5 : 1))
                    }
                    .buttonStyle(PressScale())
                    .foregroundStyle(.primary)
                    .accessibilityAddTraits(provider == p ? .isSelected : [])
                }
            }

            subhead("模型")
            VStack(spacing: 0) {
                ForEach(provider.models, id: \.self) { m in
                    choiceRow(selected: m == model) { model = m } label: {
                        Text(m).font(.callout.monospaced())
                    }
                }
            }

            subhead("音色", trailing: provider.unverified ? "列表待核"
                    : provider.moreVoices.map { "共 \($0) 种，列出常用 \(provider.voices.count) 种" } ?? "\(provider.voices.count) 种")
            VStack(spacing: 0) {
                ForEach(provider.voices) { v in
                    choiceRow(selected: v.value == voice) { voice = v.value } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(v.name).font(.callout.weight(.semibold))
                            Text(v.blurb).font(.footnote).foregroundStyle(.secondary)
                        }
                    } accessory: {
                        // Preview playback needs a provider call: next brief. The slot is reserved, greyed out.
                        Image(systemName: "play.circle")
                            .font(.title3)
                            .foregroundStyle(.tertiary)
                            .accessibilityLabel("试听（还没做）")
                    }
                }
                if let total = provider.moreVoices {
                    choiceRow(selected: false, symbol: "ellipsis.circle") {} label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("自定义音色").font(.callout.weight(.semibold))
                            Text("全部 \(total) 种，或填音色名").font(.footnote).foregroundStyle(.secondary)
                        }
                    } accessory: {
                        Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                    }
                    .id("custom")
                }
            }
        }
    }

    private func subhead(_ text: String, trailing: String? = nil) -> some View {
        HStack {
            Text(text).font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
            Spacer()
            if let trailing { Text(trailing).font(.footnote).foregroundStyle(.tertiary) }
        }
        .padding(.top, 4)
    }

    private func choiceRow<L: View, A: View>(selected: Bool, symbol: String? = nil, action: @escaping () -> Void,
                                             @ViewBuilder label: () -> L,
                                             @ViewBuilder accessory: () -> A = { EmptyView() }) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: symbol ?? (selected ? "checkmark.circle.fill" : "circle"))
                    .font(.body)
                    .foregroundStyle(selected ? AnyShapeStyle(.primary) : AnyShapeStyle(.quaternary))
                label()
                Spacer(minLength: 0)
                accessory()
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(minHeight: 44)
            .background(RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(selected ? Color.primary.opacity(scheme == .dark ? 0.08 : 0.05) : .clear))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    // MARK: API key

    private var keySection: some View {
        V2Section(look: look, symbol: "key", title: "API key", caption: provider.title) {
            if keySaved {
                HStack(alignment: .firstTextBaseline) {
                    Text("••••1234").font(.system(.title2, design: .monospaced).weight(.medium))
                    Text("已存在钥匙串").font(.footnote).foregroundStyle(.secondary)
                    Spacer()
                }
                HStack(spacing: 10) {
                    V2Pill(title: "更换", symbol: "arrow.triangle.2.circlepath") { keySaved = false }
                    V2Pill(title: "删除", symbol: "trash", destructive: true) { keySaved = false }
                }
            } else {
                SecureField("粘贴 \(provider.title) 的 API key", text: $keyDraft)
                    .textContentType(.password)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 48)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(.primary.opacity(0.06)))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(.primary.opacity(0.2), lineWidth: 1))
                V2Pill(title: "保存到钥匙串", symbol: "lock") { keySaved = true; keyDraft = "" }
            }
            Text("key 只存在这台手机的钥匙串里，开始通话时加密发给 Mac，Mac 用完即丢、不落盘。")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: 麦克风

    private var micSection: some View {
        @Bindable var controller = controller
        return V2Section(look: look, symbol: "mic", title: "麦克风", caption: "静音门限") {
            HStack(alignment: .lastTextBaseline, spacing: 8) {
                Text(controller.threshold, format: .number.precision(.fractionLength(3)))
                    .font(.system(size: 40, weight: .regular, design: .default))
                    .monospacedDigit()
                Text("默认 0.010").font(.footnote).foregroundStyle(.secondary)
                Spacer()
                V2Pill(title: "恢复默认") { controller.threshold = 0.01 }
            }
            LevelMeter(threshold: controller.threshold / 0.05, live: !reduceMotion)
            Slider(value: $controller.threshold, in: 0.001...0.05) { Text("静音门限") }
            Text("电平条越过竖线的声音才发给 Mac。环境吵、助手老被打断就调高；说话被吞字就调低。")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: 配对

    private var pairSection: some View {
        let paired = controller.link != .unpaired
        return V2Section(look: look, symbol: "desktopcomputer", title: "配对", caption: paired ? "已配对" : "未配对") {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 12) {
                GridRow { infoCell("Mac", paired ? DesignSample.macName : "—"); infoCell("配对时间", paired ? DesignSample.pairedAt : "—") }
                GridRow { infoCell("中继", "r.maruiao.top"); infoCell("加密", "端到端") }
            }
            Divider()
            V2Pill(title: "重新配对", symbol: "qrcode.viewfinder", action: onRepair)
        }
    }

    private func infoCell(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label).font(.footnote).foregroundStyle(.secondary)
            Text(value).font(.callout.weight(.medium))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    // MARK: 关于

    private var aboutSection: some View {
        V2Section(look: look, symbol: "info.circle", title: "关于", caption: "版本 \(Self.version)") {
            Text("形象素材：Claude大小姐的日记本（github.com/opsCar/claude-daxiaojie-assets）· 角色设定：ZipZipPipe（胖次狐）· 设定图：lmy414/ai-chan-sheets · CC BY-NC-SA 4.0。水彩 / 霓虹两组由 gpt-image-2.5 基于原素材生成。")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    private static var version: String {
        let v = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
        let b = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "?"
        return "\(v)（\(b)）"
    }
}

/// One persona group in the carousel: big idle portrait on its own tint, name underneath.
private struct PersonaCard: View {
    let style: PersonaStyle
    let selected: Bool
    let size: CGSize
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var tint: Color {
        switch style {
        case .pixel: Color(red: 0.96, green: 0.55, blue: 0.3)
        case .chibi: Color(red: 0.97, green: 0.5, blue: 0.62)
        case .watercolor: Color(red: 0.42, green: 0.66, blue: 0.7)
        case .neon: Color(red: 0.55, green: 0.35, blue: 0.98)
        }
    }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 22, style: .continuous)
        Button(action: action) {
            VStack(alignment: .leading, spacing: 10) {
                ZStack(alignment: .bottom) {
                    LinearGradient(colors: [tint.opacity(scheme == .dark ? 0.55 : 0.4), tint.opacity(0.08)],
                                   startPoint: .top, endPoint: .bottom)
                    GrainOverlay(opacity: 0.08)
                    if style.available {
                        Image(PersonaStyle.name(style, .idle))
                            .resizable()
                            .scaledToFit()
                            .padding(.top, 10)
                    } else {
                        VStack(spacing: 6) {
                            Image(systemName: "paintbrush.pointed").font(.title2)
                            Text("待补").font(.footnote.weight(.semibold))
                        }
                        .foregroundStyle(.secondary)
                        .frame(maxHeight: .infinity)
                    }
                }
                .frame(width: size.width, height: size.height)
                .clipShape(shape)
                .overlay(shape.strokeBorder(selected ? Color.primary : .white.opacity(scheme == .dark ? 0.1 : 0.6),
                                            lineWidth: selected ? 2 : 1))
                .overlay(alignment: .topTrailing) {
                    if selected {
                        Image(systemName: "checkmark.circle.fill")
                            .font(.title3)
                            .symbolRenderingMode(.palette)
                            .foregroundStyle(.white, Color.black.opacity(0.75))
                            .padding(8)
                    }
                }
                VStack(alignment: .leading, spacing: 1) {
                    Text(style.title).font(.callout.weight(.semibold))
                    Text(style.available ? style.blurb : "素材还没到").font(.caption).foregroundStyle(.secondary)
                }
                .padding(.leading, 4)
            }
            .keyframeAnimator(initialValue: 1.0, repeating: selected && !reduceMotion) { view, s in
                view.scaleEffect(s)
            } keyframes: { _ in
                // the picked card breathes very slightly
                CubicKeyframe(1.025, duration: 1.4)
                CubicKeyframe(1.0, duration: 1.4)
            }
        }
        .buttonStyle(PressScale())
        .foregroundStyle(.primary)
        .opacity(style.available ? 1 : 0.7)
        .accessibilityLabel("\(style.title)形象\(style.available ? "" : "，素材待补")")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Live input level with the threshold marked on it.
private struct LevelMeter: View {
    let threshold: Double
    let live: Bool

    var body: some View {
        TimelineView(.animation(paused: !live)) { ctx in
            let level = live ? fakeLevel(ctx.date.timeIntervalSinceReferenceDate) * 0.8 : 0.4
            GeometryReader { geo in
                let w = geo.size.width
                ZStack(alignment: .leading) {
                    Capsule().fill(.primary.opacity(0.08))
                    Capsule()
                        .fill(LinearGradient(colors: [.green, .yellow], startPoint: .leading, endPoint: .trailing))
                        .frame(width: max(8, w * level))
                        .opacity(level > threshold ? 1 : 0.4)
                    Rectangle().fill(.primary).frame(width: 2, height: 18).offset(x: w * min(1, threshold) - 1)
                }
            }
            .frame(height: 10)
        }
        .padding(.vertical, 4)
        .accessibilityLabel("实时电平")
    }
}
