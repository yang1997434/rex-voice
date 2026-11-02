import SwiftUI

/// The Liquid Glass design drafts. They compile into every build so the archive proves the glass APIs
/// against the device SDK, but only a DEBUG build with `REXVOICE_DESIGN=a|b|c` ever shows one.
enum DesignVariant: String {
    case a, b, c

    #if DEBUG
    static var current: DesignVariant? {
        ProcessInfo.processInfo.environment["REXVOICE_DESIGN"].flatMap(DesignVariant.init(rawValue:))
    }
    #endif
}

/// Which screen a design preview opens on (`REXVOICE_PREVIEW`); `settings` / `pair` open that sheet.
enum PreviewScreen: String {
    case unpaired, connecting, offline, idle, listening, thinking, speaking, settings, pair

    #if DEBUG
    static var current: PreviewScreen? {
        ProcessInfo.processInfo.environment["REXVOICE_PREVIEW"].flatMap(PreviewScreen.init(rawValue:))
    }
    #endif
}

enum DesignSheet: String, Identifiable {
    case settings, pair
    var id: String { rawValue }
}

struct DesignRoot: View {
    let variant: DesignVariant
    var initialSheet: DesignSheet?

    var body: some View {
        switch variant {
        case .a: PhoneDesign(initialSheet: initialSheet)
        case .b: ChatDesign(initialSheet: initialSheet)
        case .c: DashboardDesign(initialSheet: initialSheet)
        }
    }
}

/// What the screen shows, read off CallController's link + call state (same mapping as `statusText`).
enum CallPhase: Equatable {
    case unpaired, connecting, offline, idle, dialing, listening, thinking, speaking

    @MainActor init(_ c: CallController) {
        switch c.link {
        case .unpaired: self = .unpaired
        case .connecting: self = .connecting
        case .macOffline: self = .offline
        case .ready:
            guard c.inCall else { self = .idle; return }
            switch c.callState {
            case "listening": self = .listening
            case "thinking": self = .thinking
            case "speaking": self = .speaking
            default: self = .dialing
            }
        }
    }

    var label: String {
        switch self {
        case .unpaired: "未配对"
        case .connecting: "连接中"
        case .offline: "Mac 不在线"
        case .idle: "待机"
        case .dialing: "接通中"
        case .listening: "在听"
        case .thinking: "在想"
        case .speaking: "在说"
        }
    }

    var hint: String {
        switch self {
        case .unpaired: "先扫 Mac 上的配对二维码"
        case .connecting: "正在连中继…"
        case .offline: "Mac 上的 rex-voiced 没连上，会自动重试"
        case .idle: "Mac 在线，随时可以打"
        case .dialing: "正在接通语音模型…"
        case .listening: "你说，我在听"
        case .thinking: "在想怎么回…"
        case .speaking: "开口就能打断它"
        }
    }

    var color: Color {
        switch self {
        case .unpaired: .indigo
        case .connecting: .gray
        case .offline: .red
        case .idle: .blue
        case .dialing: .teal
        case .listening: .green
        case .thinking: .purple
        case .speaking: .orange
        }
    }

    var symbol: String {
        switch self {
        case .unpaired: "qrcode.viewfinder"
        case .connecting: "antenna.radiowaves.left.and.right"
        case .offline: "desktopcomputer.trianglebadge.exclamationmark"
        case .idle: "phone.fill"
        case .dialing: "phone.connection.fill"
        case .listening: "ear"
        case .thinking: "ellipsis"
        case .speaking: "speaker.wave.2.fill"
        }
    }

    var inCall: Bool { [.dialing, .listening, .thinking, .speaking].contains(self) }
    /// Audio is flowing one way or the other, so the wave moves.
    var live: Bool { self == .listening || self == .speaking }
}

extension CallController {
    var phase: CallPhase { CallPhase(self) }
    /// The main button also works unpaired: it opens the scanner instead of calling.
    var primaryEnabled: Bool { canToggleCall || link == .unpaired }
}

/// Placeholder pairing details until the next brief stores them with the pairing.
enum DesignSample {
    static let macName = "Andy 的 Mac mini"
    static let pairedAt = "11 月 1 日 21:30"
}

extension View {
    /// Same 5-second auto-hide as ContentView.
    func autoHideNotice(_ controller: CallController) -> some View {
        task(id: controller.notice) {
            guard controller.notice != nil else { return }
            try? await Task.sleep(for: .seconds(5))
            if !Task.isCancelled { controller.notice = nil }
        }
    }
}

@MainActor func primaryAction(_ controller: CallController, openPair: () -> Void) {
    if controller.link == .unpaired, !controller.inCall {
        openPair()
    } else {
        Task { await controller.toggleCall() }
    }
}

/// Bars that move while audio flows; frozen under Reduce Motion or when idle.
struct WaveBars: View {
    var live: Bool
    var count = 5
    var height: CGFloat = 22
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(paused: !live || reduceMotion)) { ctx in
            let t = ctx.date.timeIntervalSinceReferenceDate
            HStack(spacing: 3) {
                ForEach(0..<count, id: \.self) { i in
                    let level = live && !reduceMotion ? 0.3 + 0.7 * abs(sin(t * 3.1 + Double(i) * 0.9)) : (live ? 0.7 : 0.25)
                    Capsule().frame(width: 3, height: max(4, height * level))
                }
            }
            .frame(height: height)
        }
        .accessibilityHidden(true)
    }
}

/// Tap-to-dismiss notice in a glass capsule.
struct NoticePill: View {
    let text: String
    var onTap: () -> Void

    var body: some View {
        Label(text, systemImage: "info.circle")
            .font(.footnote.weight(.medium))
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .glassEffect(.regular.interactive(), in: .capsule)
            .onTapGesture(perform: onTap)
            .transition(.opacity.combined(with: .move(edge: .top)))
    }
}

struct EmptyHint: View {
    let phase: CallPhase
    var body: some View {
        Text(phase == .unpaired ? "点「配对」扫 Mac 上的二维码" : "通话内容会显示在这里")
            .font(.subheadline)
            .foregroundStyle(.secondary)
    }
}

func elapsed(since start: Date?, now: Date) -> String {
    guard let start else { return "--:--" }
    let s = max(0, Int(now.timeIntervalSince(start)))
    return String(format: "%02d:%02d", s / 60, s % 60)
}
