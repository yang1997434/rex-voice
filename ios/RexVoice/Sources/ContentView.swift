import AVFoundation
import SwiftUI

struct ContentView: View {
    @Environment(CallController.self) private var controller
    @State private var scanning = false
    @State private var settings = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                header
                transcript
                callButton
            }
            .navigationTitle("Rex Voice")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("设置", systemImage: "slider.horizontal.3") { settings = true }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("配对", systemImage: "qrcode.viewfinder") { scanning = true }
                }
            }
            .sheet(isPresented: $scanning) { scanner }
            .sheet(isPresented: $settings) { SettingsView() }
        }
        .task(id: controller.notice) {
            guard controller.notice != nil else { return }
            try? await Task.sleep(for: .seconds(5))
            if !Task.isCancelled { controller.notice = nil }
        }
    }

    private var statusColor: Color {
        switch controller.link {
        case .unpaired, .macOffline: .orange
        case .connecting: .gray
        case .ready: controller.inCall ? .green : .blue
        }
    }

    private var header: some View {
        VStack(spacing: 6) {
            HStack(spacing: 8) {
                Circle().fill(statusColor).frame(width: 10, height: 10).accessibilityHidden(true)
                Text(controller.statusText).font(.headline)
            }
            if let notice = controller.notice {
                Text(notice)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .onTapGesture { controller.notice = nil }
            }
        }
        .frame(maxWidth: .infinity)
        .padding()
        .animation(.default, value: controller.notice)
    }

    private var transcript: some View {
        ScrollView {
            LazyVStack(spacing: 8) {
                ForEach(controller.transcript) { line in
                    Text(line.text)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                        .background(line.fromUser ? Color.accentColor.opacity(0.15) : Color(.secondarySystemBackground),
                                    in: .rect(cornerRadius: 12))
                        .frame(maxWidth: .infinity, alignment: line.fromUser ? .trailing : .leading)
                        .accessibilityLabel((line.fromUser ? "我：" : "助手：") + line.text)
                }
            }
            .padding(.horizontal)
        }
        .defaultScrollAnchor(.bottom)
        .overlay {
            if controller.transcript.isEmpty {
                Text(controller.link == .unpaired ? "点右上角「配对」扫 Mac 上的二维码" : "通话内容会显示在这里")
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var callButton: some View {
        Button {
            Task { await controller.toggleCall() }
        } label: {
            Text(controller.inCall ? "挂断" : "开始通话")
                .font(.title2.bold())
                .frame(maxWidth: .infinity, minHeight: 64)
        }
        .buttonStyle(.borderedProminent)
        .tint(controller.inCall ? .red : .green)
        .disabled(!controller.canToggleCall)
        .padding()
    }

    private var scanner: some View {
        ScannerSheet(onCode: { code in
            scanning = false
            do {
                try controller.pair(with: code)
                controller.notice = "配对成功，正在连接"
            } catch {
                controller.notice = "二维码不对：\(error.localizedDescription)"
            }
        }, onError: { message in
            scanning = false
            controller.notice = message
        })
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

private struct SettingsView: View {
    @Environment(CallController.self) private var controller
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        @Bindable var controller = controller
        NavigationStack {
            Form {
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
            }
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } }
            }
        }
    }
}
