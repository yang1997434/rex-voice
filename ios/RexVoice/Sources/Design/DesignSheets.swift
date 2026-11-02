import SwiftUI

/// Settings layout for the drafts. Provider / model / voice / key are layout only here (local state);
/// wiring them to Keychain and call.start is the next brief.
struct DesignSettings: View {
    let tint: Color
    /// Variant A paints its gradient behind the form; B and C keep the system grouped background.
    var tintedBackground = false
    var onRepair: () -> Void

    @Environment(CallController.self) private var controller
    @Environment(\.dismiss) private var dismiss
    @State private var provider = Provider.openai
    @State private var model = Provider.openai.models[0]
    @State private var voice = Provider.openai.voices[0]
    @State private var editingKey = false
    @State private var keyDraft = ""

    enum Provider: String, CaseIterable, Identifiable {
        case openai, qwen, doubao
        var id: String { rawValue }
        var title: String {
            switch self {
            case .openai: "OpenAI"
            case .qwen: "通义 Qwen"
            case .doubao: "豆包"
            }
        }
        // shortcut: placeholder model / voice names for the draft; the provider brief fixes the real lists.
        var models: [String] {
            switch self {
            case .openai: ["gpt-realtime", "gpt-realtime-mini"]
            case .qwen: ["qwen3-omni-flash-realtime"]
            case .doubao: ["doubao-realtime"]
            }
        }
        var voices: [String] {
            switch self {
            case .openai: ["marin", "cedar", "alloy"]
            case .qwen: ["Cherry", "Ethan", "Serena"]
            case .doubao: ["灿灿", "爽快思思"]
            }
        }
    }

    var body: some View {
        @Bindable var controller = controller
        NavigationStack {
            Form {
                Section {
                    GlassEffectContainer(spacing: 8) {
                        HStack(spacing: 8) {
                            ForEach(Provider.allCases) { p in
                                Button {
                                    provider = p
                                    model = p.models[0]
                                    voice = p.voices[0]
                                } label: {
                                    Text(p.title)
                                        .font(.subheadline.weight(provider == p ? .semibold : .regular))
                                        .frame(maxWidth: .infinity, minHeight: 36)
                                }
                                .buttonStyle(.plain)
                                .foregroundStyle(provider == p ? .white : .primary)
                                .glassEffect(provider == p ? .regular.tint(tint).interactive() : .regular.interactive(),
                                             in: .capsule)
                                .accessibilityAddTraits(provider == p ? .isSelected : [])
                            }
                        }
                    }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                } header: {
                    Text("语音供应商")
                }

                Section {
                    Picker("模型", selection: $model) {
                        ForEach(provider.models, id: \.self) { Text($0) }
                    }
                    Picker("音色", selection: $voice) {
                        ForEach(provider.voices, id: \.self) { Text($0) }
                    }
                    if editingKey {
                        SecureField("粘贴 \(provider.title) 的 API key", text: $keyDraft)
                            .textContentType(.password)
                        Button("保存到钥匙串") { editingKey = false; keyDraft = "" }
                    } else {
                        LabeledContent("API key") {
                            HStack(spacing: 8) {
                                Text("已保存 ••••1234").foregroundStyle(.secondary).monospacedDigit()
                                Button("更换") { editingKey = true }
                                    .buttonStyle(.glass)
                                    .controlSize(.small)
                            }
                        }
                    }
                } header: {
                    Text(provider.title)
                } footer: {
                    Text("key 只存在这台手机的钥匙串里，开始通话时加密发给 Mac，Mac 用完即丢、不落盘。")
                }

                Section {
                    Slider(value: $controller.threshold, in: 0.001...0.05) {
                        Text("静音门限")
                    }
                    Text("门限 \(controller.threshold, format: .number.precision(.fractionLength(3)))，默认 0.010")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Button("恢复默认") { controller.threshold = 0.01 }
                } header: {
                    Text("静音门限")
                } footer: {
                    Text("比门限还小的声音不发给 Mac。环境吵、助手老被打断就调高；说话被吞字就调低。")
                }

                Section("配对") {
                    LabeledContent("Mac", value: controller.link == .unpaired ? "未配对" : DesignSample.macName)
                    LabeledContent("配对时间", value: controller.link == .unpaired ? "—" : DesignSample.pairedAt)
                    Button("重新配对", systemImage: "qrcode.viewfinder", action: onRepair)
                }

                Section {
                    LabeledContent("版本", value: Self.version)
                } header: {
                    Text("关于")
                } footer: {
                    Text("App 图标用 gpt-image-2.5 基于「Claude大小姐」半身素材生成。素材：Claude大小姐的日记本（github.com/opsCar/claude-daxiaojie-assets）· 角色设定：ZipZipPipe（胖次狐）· 设定图：lmy414/ai-chan-sheets · CC BY-NC-SA 4.0。")
                }
            }
            .scrollContentBackground(tintedBackground ? .hidden : .automatic)
            .background {
                if tintedBackground {
                    LinearGradient(colors: [tint.opacity(0.35), Color(.systemGroupedBackground)],
                                   startPoint: .top, endPoint: .center)
                        .ignoresSafeArea()
                }
            }
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成", systemImage: "checkmark") { dismiss() }
                }
            }
        }
        .tint(tint)
    }

    private static var version: String {
        let v = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
        let b = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "?"
        return "\(v)（\(b)）"
    }
}

/// Scanner page: camera (or a stand-in in previews), a viewfinder, one line of guidance on glass.
struct DesignPairPage: View {
    let tint: Color
    @Environment(CallController.self) private var controller
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ZStack {
            camera.ignoresSafeArea()
            ViewfinderCorners()
                .stroke(tint, style: StrokeStyle(lineWidth: 5, lineCap: .round))
                .frame(width: 260, height: 260)
                .shadow(color: tint.opacity(0.6), radius: 12)
                .accessibilityHidden(true)
        }
        .overlay(alignment: .topTrailing) {
            Button("关闭", systemImage: "xmark") { dismiss() }
                .labelStyle(.iconOnly)
                .font(.title3)
                .buttonStyle(.glass)
                .buttonBorderShape(.circle)
                .controlSize(.large)
                .padding()
        }
        .overlay(alignment: .bottom) {
            VStack(spacing: 6) {
                Text("扫 Mac 上的配对二维码").font(.headline)
                Text("在 Mac 终端运行 rex-voiced pair，把二维码放进框里")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(.horizontal, 22)
            .padding(.vertical, 16)
            .glassEffect(.regular, in: .rect(cornerRadius: 28))
            .padding(.horizontal, 24)
            .padding(.bottom, 28)
        }
        .environment(\.colorScheme, .dark)
    }

    @ViewBuilder private var camera: some View {
        #if DEBUG
        if PreviewScreen.current != nil {
            // Simulator has no camera: a dim scene with a QR code standing in for the feed.
            ZStack {
                LinearGradient(colors: [Color(white: 0.22), Color(white: 0.05)], startPoint: .top, endPoint: .bottom)
                Image(systemName: "qrcode")
                    .resizable()
                    .frame(width: 180, height: 180)
                    .foregroundStyle(.white.opacity(0.85))
            }
        } else {
            scanner
        }
        #else
        scanner
        #endif
    }

    private var scanner: some View {
        ScannerSheet(onCode: { code in
            dismiss()
            do {
                try controller.pair(with: code)
                controller.notice = "配对成功，正在连接"
            } catch {
                controller.notice = "二维码不对：\(error.localizedDescription)"
            }
        }, onError: { message in
            dismiss()
            controller.notice = message
        })
    }
}

/// Four rounded corner brackets of a scan frame.
private struct ViewfinderCorners: Shape {
    func path(in r: CGRect) -> Path {
        let arm: CGFloat = 48, radius: CGFloat = 28
        var p = Path()
        for (corner, dx, dy) in [(CGPoint(x: r.minX, y: r.minY), 1.0, 1.0), (CGPoint(x: r.maxX, y: r.minY), -1.0, 1.0),
                                 (CGPoint(x: r.maxX, y: r.maxY), -1.0, -1.0), (CGPoint(x: r.minX, y: r.maxY), 1.0, -1.0)] {
            p.move(to: CGPoint(x: corner.x, y: corner.y + dy * arm))
            p.addArc(tangent1End: corner, tangent2End: CGPoint(x: corner.x + dx * arm, y: corner.y), radius: radius)
            p.addLine(to: CGPoint(x: corner.x + dx * arm, y: corner.y))
        }
        return p
    }
}
