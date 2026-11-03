import SwiftUI

/// The thick small-card surface (transcript card, settings cards): top-lit edge, inner glow, two-layer shadow.
struct CardSurface: View {
    var radius: CGFloat = 26
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: radius, style: .continuous)
        let dark = scheme == .dark
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

/// Hark-style section: small icon chip + "标题 · 说明", then content.
struct V2Section<Content: View>: View {
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
                    Text("· \(caption)").font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            content
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(CardSurface())
    }
}

/// Light pill button like Hark's "Pay Now".
struct V2Pill: View {
    let title: String
    var symbol: String?
    var destructive = false
    /// A small spinner in place of the symbol.
    var busy = false
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.isEnabled) private var enabled

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if busy {
                    ProgressView().controlSize(.small)
                } else if let symbol {
                    Image(systemName: symbol)
                }
                Text(title)
            }
            .opacity(enabled ? 1 : 0.4)
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

/// Settings: a page of thick cards — persona carousel, provider / model / voice, API key, mic gate, pairing, about.
struct SettingsPage: View {
    var onRepair: () -> Void

    @Environment(CallController.self) private var controller
    @Environment(AppSettings.self) private var settings
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    @State private var editingKey = false
    @State private var keyDraft = ""
    @State private var keyError: String?
    @State private var custom: CustomField?
    @State private var customDraft = ""

    enum CustomField: Identifiable {
        case model, voice
        var id: Self { self }
    }

    private var provider: VoiceProvider { settings.provider }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(spacing: 14) {
                    basicSection
                    personaSection.id("persona")
                    voiceSection.id("voices")
                    keySection.id("key")
                    micSection
                    pairSection
                    aboutSection.id("bottom")
                }
                .padding(.horizontal, 16)
                .padding(.top, 4)
                .padding(.bottom, 40)
                .frame(maxWidth: 640) // Duo unfolded / landscape: a centered column, cards not stretched
                .frame(maxWidth: .infinity)
            }
            .scrollIndicators(.hidden)
            // Pinned bar: content scrolls under it (soft edge) instead of under the status bar.
            .safeAreaBar(edge: .top) { header }
            #if DEBUG
            .task {
                // `REXVOICE_SCROLL=voices|custom|key|bottom` lands a screenshot there.
                guard let anchor = ProcessInfo.processInfo.environment["REXVOICE_SCROLL"] else { return }
                try? await Task.sleep(for: .milliseconds(400))
                proxy.scrollTo(anchor, anchor: anchor == "custom" ? .bottom : .top)
            }
            #endif
        }
        .background { pageBackground }
        .onChange(of: settings.provider) {
            editingKey = false
            keyDraft = ""
            keyError = nil
        }
        .alert(custom == .model ? "自定义模型" : "自定义音色", isPresented: Binding(get: { custom != nil },
                                                                           set: { if !$0 { custom = nil } })) {
            TextField(custom == .model ? "模型名，原样发给 Mac" : "音色名，原样发给 Mac", text: $customDraft)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            Button("用这个") {
                let value = customDraft.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !value.isEmpty, let field = custom else { return }
                if field == .model { settings.setModel(value, for: provider) } else { settings.setVoice(value, for: provider) }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("\(provider.title) 认不认这个名字由它自己的接口决定，填错了通话会报错。")
        }
    }

    private var header: some View {
        HStack(spacing: 12) {
            V2IconButton(title: "返回", symbol: "chevron.left") { dismiss() }
            Text("设置").font(.system(.largeTitle, design: .serif).weight(.semibold))
            Spacer()
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .padding(.bottom, 8)
        .frame(maxWidth: 640)
        .frame(maxWidth: .infinity)
    }

    private var pageBackground: some View {
        ZStack {
            (scheme == .dark ? Color(white: 0.04) : Color(white: 0.95))
            RadialGradient(colors: [Color(red: 0.94, green: 0.63, blue: 0.49).opacity(scheme == .dark ? 0.18 : 0.22), .clear],
                           center: .top, startRadius: 0, endRadius: 420)
            GrainOverlay(opacity: 0.05)
        }
        .ignoresSafeArea()
    }

    // MARK: 基本

    private var basicSection: some View {
        @Bindable var settings = settings
        return V2Section(symbol: "person.text.rectangle", title: "基本") {
            VStack(alignment: .leading, spacing: 8) {
                Text("称呼").font(.subheadline.weight(.medium))
                TextField("Mac 配置里的名字", text: $settings.name)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 48)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(.primary.opacity(0.06)))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(.primary.opacity(0.2), lineWidth: 1))
                    .accessibilityLabel("称呼")
                Text("助手怎么叫你，下次通话和试听生效；空着就用 Mac 配置里的名字。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Toggle("Claude 等我时推送提醒", isOn: $settings.pushEnabled)
                    .toggleStyle(.switch)
                    .font(.subheadline.weight(.medium))
                    .padding(.top, 8)
                Text("关掉后只在 app 里显示提问卡、通话里照样提醒；在电脑前可以先关掉。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: 形象

    private var personaSection: some View {
        @Bindable var settings = settings
        let current = settings.persona.id
        return V2Section(symbol: "person.crop.square", title: "形象", caption: "通话时她会跟着换表情") {
            ScrollViewReader { carousel in
                ScrollView(.horizontal) {
                    LazyHStack(spacing: 12) {
                        ForEach(Persona.available) { p in
                            PersonaCard(persona: p, selected: p.id == current, size: CGSize(width: 200, height: 240)) {
                                settings.personaID = p.id
                            }
                            .id(p.id)
                        }
                    }
                    .scrollTargetLayout()
                    .padding(.vertical, 6)
                }
                .scrollTargetBehavior(.viewAligned)
                .contentMargins(.horizontal, 18, for: .scrollContent)
                .scrollIndicators(.hidden)
                .padding(.horizontal, -18)
                .onAppear { carousel.scrollTo(current, anchor: .center) } // open on the current pick
            }
        }
    }

    // MARK: 声音

    private var voiceSection: some View {
        let model = settings.model(for: provider)
        let voice = settings.voice(for: provider)
        let voices = provider.voices(model: model)
        let customModel = !provider.models.contains(model)
        let customVoice = !voices.contains { $0.value == voice }
        return V2Section(symbol: "waveform", title: "声音", caption: provider.title) {
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(VoiceProvider.allCases) { p in
                    Button { settings.provider = p } label: {
                        HStack(spacing: 8) {
                            ProviderLogo(provider: p, size: 24)
                            Text(p.shortName).font(.subheadline.weight(.medium)).lineLimit(1)
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
                    choiceRow(selected: m == model) { settings.setModel(m, for: provider) } label: {
                        Text(m).font(.callout.monospaced()).lineLimit(1).minimumScaleFactor(0.7)
                    }
                }
                customRow(selected: customModel, value: customModel ? model : nil, empty: "填模型名") {
                    customDraft = customModel ? model : ""
                    custom = .model
                }
            }

            // MOSS-Live has no voices to pick and no text input for the Mac to preview with.
            if provider != .moss {
                HStack(spacing: 12) {
                    subhead("音色", trailing: provider.totalVoices.map { "共 \($0) 种，列出常用 \(voices.count) 种" } ?? "\(voices.count) 种")
                    previewButton
                }
                if case let .failed(reason) = controller.previewState {
                    Text("试听失败：\(reason)")
                        .font(.footnote)
                        .foregroundStyle(.red)
                        .lineLimit(1)
                } else if controller.inCall {
                    Text("通话中不能试听").font(.footnote).foregroundStyle(.secondary)
                }
                VStack(spacing: 0) {
                    // Grok's list is grouped by gender (女声 / 男声 / not labelled); the others have no gender data.
                    let groups = Dictionary(grouping: voices) { $0.gender ?? "" }
                    let order = provider == .grok ? ["女声", "男声", ""] : [""]
                    ForEach(order, id: \.self) { g in
                        if provider == .grok {
                            Text(g.isEmpty ? "未标性别" : g)
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(.tertiary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, 12)
                                .padding(.top, 10)
                                .padding(.bottom, 2)
                        }
                        ForEach(groups[g] ?? []) { v in
                            choiceRow(selected: v.value == voice) { settings.setVoice(v.value, for: provider) } label: {
                                Text(v.name).font(.callout.weight(.semibold))
                            }
                        }
                    }
                    customRow(selected: customVoice, value: customVoice ? voice : nil, empty: "填音色名") {
                        customDraft = customVoice ? voice : ""
                        custom = .voice
                    }
                    .id("custom")
                }
            }
        }
    }

    /// 试听: the Mac speaks one line in the picked voice. Grey mid-call (the voice cannot change then) or with the Mac offline.
    private var previewButton: some View {
        let (title, symbol): (String, String?) = switch controller.previewState {
        case .connecting: ("连接中", nil)
        case .playing: ("播放中", "waveform")
        case .idle, .failed: ("试听", "play.fill")
        }
        return V2Pill(title: title, symbol: symbol, busy: controller.previewState == .connecting) {
            Task { await controller.previewVoice() }
        }
        .disabled(!controller.canPreview)
        .accessibilityHint(controller.inCall ? "通话中不能试听" : controller.canPreview ? "听一句当前音色" : "Mac 不在线")
    }

    private func customRow(selected: Bool, value: String?, empty: String, action: @escaping () -> Void) -> some View {
        choiceRow(selected: selected, symbol: selected ? nil : "ellipsis.circle", action: action) {
            VStack(alignment: .leading, spacing: 2) {
                Text("自定义…").font(.callout.weight(.semibold))
                Text(value ?? empty).font(value == nil ? .footnote : .footnote.monospaced()).foregroundStyle(.secondary)
            }
        } accessory: {
            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
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
        V2Section(symbol: "key", title: "API key", caption: provider.title) {
            if let tail = settings.keyTails[provider], !editingKey {
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text("已保存").font(.subheadline.weight(.medium)).foregroundStyle(.secondary)
                    Text("••••\(tail)").font(.system(.title2, design: .monospaced).weight(.medium))
                    Spacer()
                }
                .accessibilityElement(children: .combine)
                HStack(spacing: 10) {
                    V2Pill(title: "更换", symbol: "arrow.triangle.2.circlepath") { editingKey = true }
                    V2Pill(title: "删除", symbol: "trash", destructive: true) {
                        do { try settings.deleteKey(for: provider) } catch { keyError = error.localizedDescription }
                    }
                }
            } else {
                SecureField("粘贴 \(provider.title) 的 API key", text: $keyDraft)
                    .textContentType(.password)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 48)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(.primary.opacity(0.06)))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(.primary.opacity(0.2), lineWidth: 1))
                HStack(spacing: 10) {
                    V2Pill(title: "保存到钥匙串", symbol: "lock") {
                        do {
                            try settings.saveKey(keyDraft, for: provider)
                            keyDraft = ""
                            keyError = nil
                            editingKey = false
                        } catch {
                            keyError = error.localizedDescription
                        }
                    }
                    .disabled(keyDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    if settings.keyTails[provider] != nil {
                        V2Pill(title: "取消") { editingKey = false; keyDraft = "" }
                    }
                }
            }
            if let keyError {
                Text(keyError).font(.footnote).foregroundStyle(.red)
            }
            Text(settings.keyTails[provider] == nil
                 ? "没填的话 Mac 用它自己钥匙串里的 key。key 只存在这台手机的钥匙串里，开始通话时加密发给 Mac，Mac 用完即丢、不落盘。"
                 : "key 只存在这台手机的钥匙串里，开始通话时加密发给 Mac，Mac 用完即丢、不落盘。")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: 麦克风

    private var micSection: some View {
        @Bindable var controller = controller
        return V2Section(symbol: "mic", title: "麦克风", caption: "静音门限") {
            HStack(alignment: .lastTextBaseline, spacing: 8) {
                Text(controller.threshold, format: .number.precision(.fractionLength(3)))
                    .font(.system(size: 40, weight: .regular, design: .default))
                    .monospacedDigit()
                Text("默认 0.010").font(.footnote).foregroundStyle(.secondary)
                Spacer()
                V2Pill(title: "恢复默认") { controller.threshold = 0.01 }
            }
            LevelMeter(level: Double(controller.level),
                       threshold: Double(AudioEngine.normalized(Float(controller.threshold))))
            Slider(value: $controller.threshold, in: 0.001...0.05) { Text("静音门限") }
            Text(controller.inCall ? "电平条越过竖线的声音才发给 Mac。环境吵、助手老被打断就调高；说话被吞字就调低。"
                 : "电平条越过竖线的声音才发给 Mac（通话中电平条才会动）。环境吵、助手老被打断就调高；说话被吞字就调低。")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: 配对

    private var pairSection: some View {
        let paired = controller.link != .unpaired
        return V2Section(symbol: "desktopcomputer", title: "配对", caption: paired ? "已配对" : "未配对") {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 12) {
                GridRow { infoCell("中继", paired ? controller.relayHost ?? "—" : "—"); infoCell("加密", "端到端") }
                if paired, let at = controller.pairedAt {
                    GridRow {
                        infoCell("配对时间", at.formatted(.dateTime.month().day().hour().minute()))
                        Color.clear.gridCellUnsizedAxes([.horizontal, .vertical])
                    }
                }
            }
            Divider()
            V2Pill(title: paired ? "重新配对" : "配对", symbol: "qrcode.viewfinder", action: onRepair)
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
        V2Section(symbol: "info.circle", title: "关于", caption: "版本 \(Self.version)") {
            VStack(alignment: .leading, spacing: 8) {
                Text("形象素材").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
                ForEach(Persona.all) { p in
                    Text("\(p.name)：\(p.source) · \(p.license)")
                }
                Text("CC BY-NC-SA 4.0 是非商用协议，本 app 仅内测自用。")
            }
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
    let persona: Persona
    let selected: Bool
    let size: CGSize
    let action: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 22, style: .continuous)
        Button(action: action) {
            VStack(alignment: .leading, spacing: 10) {
                ZStack(alignment: .bottom) {
                    LinearGradient(colors: [persona.tintColor.opacity(scheme == .dark ? 0.55 : 0.4), persona.tintColor.opacity(0.08)],
                                   startPoint: .top, endPoint: .bottom)
                    GrainOverlay(opacity: 0.08)
                    Image(persona.image(.idle))
                        .resizable()
                        .scaledToFit()
                        .padding(.top, 10)
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
                    Text(persona.name).font(.callout.weight(.semibold))
                    if let blurb = persona.blurb { Text(blurb).font(.caption).foregroundStyle(.secondary) }
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
        .accessibilityLabel("\(persona.name)形象")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Mic level (0...1, `AudioEngine.normalized`) with the threshold marked on it; lit only past the threshold.
private struct LevelMeter: View {
    let level: Double
    let threshold: Double

    var body: some View {
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
            .animation(.linear(duration: 0.05), value: level)
        }
        .frame(height: 10)
        .padding(.vertical, 4)
        .accessibilityElement()
        .accessibilityLabel("实时电平")
        .accessibilityValue(level > threshold ? "过门限" : "低于门限")
    }
}
