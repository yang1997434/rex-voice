import SwiftUI

/// Scanner page for D / E: viewfinder up top, the persona at the bottom pointing at it with the instructions.
struct V2PairPage: View {
    let look: V2Look
    @Environment(CallController.self) private var controller
    @Environment(\.dismiss) private var dismiss
    @AppStorage(PersonaStyle.storageKey) private var persona = PersonaStyle.pixel

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .bottom) {
                camera(geo.size)
                LinearGradient(colors: [.clear, .black.opacity(0.75)], startPoint: .center, endPoint: .bottom)
                    .allowsHitTesting(false)
                Viewfinder()
                    .frame(width: 250, height: 250)
                    .position(x: geo.size.width / 2, y: geo.size.height * 0.34)
                guide
            }
        }
        .ignoresSafeArea()
        .overlay(alignment: .topTrailing) {
            V2IconButton(title: "关闭", symbol: "xmark") { dismiss() }
                .padding()
        }
        .environment(\.colorScheme, .dark)
    }

    private var guide: some View {
        HStack(alignment: .bottom, spacing: -8) {
            PersonaView(style: persona, state: .speaking)
                .frame(width: 140, height: 140)
                .mask(LinearGradient(stops: [.init(color: .black, location: 0.7), .init(color: .clear, location: 1)],
                                     startPoint: .top, endPoint: .bottom))
            VStack(alignment: .leading, spacing: 6) {
                Text("扫 Mac 上的配对二维码").font(.headline)
                Text("在 Mac 终端运行 rex-voiced pair，把二维码放进上面的框里")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(16)
            .background(V2Surface(look: look, radius: 22))
            .padding(.bottom, 36)
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 40) // home indicator; the page ignores the safe area so camera and frame share coordinates
    }

    @ViewBuilder private func camera(_ size: CGSize) -> some View {
        #if DEBUG
        if PreviewScreen.current != nil {
            // Simulator has no camera: a dim scene with a QR code standing in for the feed.
            ZStack {
                AmbientBackground(mood: Mood(.idle, dark: true), glowCenter: UnitPoint(x: 0.5, y: 0.3))
                Image(systemName: "qrcode")
                    .resizable()
                    .frame(width: 150, height: 150)
                    .foregroundStyle(.white.opacity(0.8))
                    .position(x: size.width / 2, y: size.height * 0.34)
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

/// Soft white corner brackets with a faint glow.
private struct Viewfinder: View {
    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 36, style: .continuous)
                .strokeBorder(.white.opacity(0.12), lineWidth: 1)
            Corners()
                .stroke(.white, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                .shadow(color: .white.opacity(0.5), radius: 10)
        }
        .accessibilityHidden(true)
    }

    private struct Corners: Shape {
        func path(in r: CGRect) -> Path {
            let arm: CGFloat = 44, radius: CGFloat = 32
            var p = Path()
            for (c, dx, dy) in [(CGPoint(x: r.minX, y: r.minY), 1.0, 1.0), (CGPoint(x: r.maxX, y: r.minY), -1.0, 1.0),
                                (CGPoint(x: r.maxX, y: r.maxY), -1.0, -1.0), (CGPoint(x: r.minX, y: r.maxY), 1.0, -1.0)] {
                p.move(to: CGPoint(x: c.x, y: c.y + dy * arm))
                p.addArc(tangent1End: c, tangent2End: CGPoint(x: c.x + dx * arm, y: c.y), radius: radius)
                p.addLine(to: CGPoint(x: c.x + dx * arm, y: c.y))
            }
            return p
        }
    }
}
