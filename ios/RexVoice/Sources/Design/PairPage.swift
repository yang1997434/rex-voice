import SwiftUI

/// Scanner page: viewfinder up top, the persona at the bottom pointing at it with the instructions.
struct PairPage: View {
    @Environment(CallController.self) private var controller
    @Environment(AppSettings.self) private var settings
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        GeometryReader { geo in
            // The page ignores the safe area so camera and frame share coordinates; the frame and the guide still keep
            // clear of side insets (Duo outer screen's status column, landscape) and fit a short landscape height.
            let inset = geo.safeAreaInsets
            let side = min(250, geo.size.height * 0.45)
            let center = CGPoint(x: inset.leading + (geo.size.width - inset.leading - inset.trailing) / 2,
                                 y: geo.size.height * (geo.size.width > geo.size.height ? 0.3 : 0.34))
            ZStack(alignment: .bottom) {
                camera(center: center)
                LinearGradient(colors: [.clear, .black.opacity(0.75)], startPoint: .center, endPoint: .bottom)
                    .allowsHitTesting(false)
                Viewfinder()
                    .frame(width: side, height: side)
                    .position(center)
                guide
                    .padding(.leading, inset.leading)
                    .padding(.trailing, inset.trailing)
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
            PersonaView(persona: settings.persona, state: .speaking)
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
            .background(CardSurface(radius: 22))
            .padding(.bottom, 36)
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 40) // home indicator
    }

    @ViewBuilder private func camera(center: CGPoint) -> some View {
        #if DEBUG
        if PreviewScreen.current != nil {
            // Simulator has no camera: a dim scene with a QR code standing in for the feed.
            ZStack {
                AmbientBackground(mood: Mood(.idle, dark: true), glowCenter: UnitPoint(x: 0.5, y: 0.3))
                Image(systemName: "qrcode")
                    .resizable()
                    .frame(width: 150, height: 150)
                    .foregroundStyle(.white.opacity(0.8))
                    .position(center)
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

#if DEBUG
/// Deep gradient + soft blurred light pools + grain: the「房间」 the persona stands in.
struct AmbientBackground: View {
    let mood: Mood
    var glowCenter = UnitPoint(x: 0.5, y: 0.36)

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width, h = geo.size.height
            ZStack {
                LinearGradient(colors: [mood.top, mood.bottom], startPoint: .top, endPoint: .bottom)
                Circle().fill(mood.glow.opacity(0.55))
                    .frame(width: w * 1.1, height: w * 1.1)
                    .blur(radius: 90)
                    .position(x: w * glowCenter.x, y: h * glowCenter.y)
                Circle().fill(mood.glow.opacity(0.25))
                    .frame(width: w * 0.8, height: w * 0.8)
                    .blur(radius: 110)
                    .position(x: w * 0.05, y: h * 0.92)
                Circle().fill(mood.accent.opacity(0.18))
                    .frame(width: w * 0.7, height: w * 0.7)
                    .blur(radius: 100)
                    .position(x: w * 1.0, y: h * 0.08)
                GrainOverlay(opacity: 0.08)
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}
#endif
