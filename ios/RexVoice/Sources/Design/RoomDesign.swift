import SwiftUI

/// Draft D「房间」: the persona stands in a lit scene (deep gradient + light pools + grain). The light
/// color follows the call state, the transcript floats underneath like subtitles, settings pull up from below.
struct RoomDesign: View {
    @Environment(CallController.self) private var controller
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @AppStorage(PersonaStyle.storageKey) private var persona = PersonaStyle.pixel
    @State private var sheet: DesignSheet?

    init(initialSheet: DesignSheet? = nil) { _sheet = State(initialValue: initialSheet) }

    var body: some View {
        let phase = controller.phase
        let state = PersonaState(phase)
        let mood = Mood(state, dark: scheme == .dark)
        VStack(spacing: 0) {
            topBar
            stage(state, mood: mood)
            status(phase, mood: mood)
            Spacer(minLength: 10)
            subtitles(phase, mood: mood)
            V2CallButton(phase: phase, enabled: controller.primaryEnabled) {
                primaryAction(controller) { sheet = .pair }
            }
            .padding(.horizontal, 24)
            .padding(.top, 14)
            .padding(.bottom, 8)
        }
        .background { AmbientBackground(mood: mood) }
        .animation(reduceMotion ? .easeInOut(duration: 0.3) : .smooth(duration: 0.6), value: state)
        .animation(.smooth, value: controller.notice)
        .sheet(item: $sheet) { s in
            switch s {
            case .settings:
                V2Settings(look: .room) { sheet = .pair }
                    .presentationDetents([.medium, .large], selection: .constant(.large))
                    .presentationDragIndicator(.visible)
                    .presentationCornerRadius(36)
                    .presentationBackground { AmbientBackground(mood: Mood(.idle, dark: scheme == .dark)).opacity(0.92) }
            case .pair:
                V2PairPage(look: .room)
            }
        }
        .autoHideNotice(controller)
    }

    private var topBar: some View {
        HStack(spacing: 10) {
            Wordmark()
            Spacer()
            V2IconButton(title: "配对", symbol: "qrcode.viewfinder") { sheet = .pair }
            V2IconButton(title: "设置", symbol: "slider.horizontal.3") { sheet = .settings }
        }
        .padding(.horizontal, 20)
        .padding(.top, 4)
    }

    /// Persona on a lit floor: a glow pool behind, halo rings while listening, the bust fading into the floor.
    private func stage(_ state: PersonaState, mood: Mood) -> some View {
        ZStack(alignment: .bottom) {
            Ellipse()
                .fill(mood.glow.opacity(scheme == .dark ? 0.45 : 0.55))
                .frame(width: 260, height: 46)
                .blur(radius: 24)
                .offset(y: 6)
            VoiceHalo(active: state == .listening, color: mood.accent)
                .padding(.horizontal, 10)
            PersonaView(style: persona, state: state)
                .mask(LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black, location: 0.74),
                                             .init(color: .clear, location: 1)],
                                     startPoint: .top, endPoint: .bottom))
                // busts are cropped square; soften the side edges so the hair melts into the light
                .mask(LinearGradient(stops: [.init(color: .clear, location: 0), .init(color: .black, location: 0.14),
                                             .init(color: .black, location: 0.86), .init(color: .clear, location: 1)],
                                     startPoint: .leading, endPoint: .trailing))
                .saturation(state == .offline ? 0.35 : 1)
                .opacity(state == .offline ? 0.75 : 1)
                .shadow(color: mood.glow.opacity(0.35), radius: 30)
        }
        .containerRelativeFrame(.vertical) { h, _ in h * 0.46 }
        .frame(maxWidth: .infinity)
        .padding(.top, 8)
    }

    private func status(_ phase: CallPhase, mood: Mood) -> some View {
        VStack(spacing: 6) {
            HStack(spacing: 8) {
                if phase.live { WaveBars(live: true, count: 4, height: 16).foregroundStyle(mood.accent) }
                Text(phase.label)
                    .font(.system(.title, design: .serif).weight(.semibold))
                    .contentTransition(.opacity)
            }
            Text("\(controller.link == .unpaired ? "还没配对 Mac" : DesignSample.macName) · \(phase.hint)")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            if let notice = controller.notice {
                NoticePill(text: notice) { controller.notice = nil }
                    .padding(.top, 6)
            }
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal, 20)
        .padding(.top, 10)
    }

    /// Last three lines as subtitles: newest brightest, older ones fading out above it.
    private func subtitles(_ phase: CallPhase, mood: Mood) -> some View {
        let lines = Array(controller.transcript.suffix(3))
        return VStack(spacing: 10) {
            if lines.isEmpty {
                EmptyHint(phase: phase)
            }
            ForEach(Array(lines.enumerated()), id: \.element.id) { i, line in
                let age = lines.count - 1 - i
                let who = Text(line.fromUser ? "我  " : "Rex  ")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(line.fromUser ? AnyShapeStyle(.secondary) : AnyShapeStyle(mood.accent))
                Text("\(who)\(Text(line.text).font(age == 0 ? .callout.weight(.medium) : .footnote))")
                    .multilineTextAlignment(.center)
                    .opacity(age == 0 ? 1 : (age == 1 ? 0.62 : 0.36))
                    .transition(.opacity.combined(with: .move(edge: .bottom)))
                    .accessibilityLabel((line.fromUser ? "我：" : "Rex：") + line.text)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
        .background {
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .fill(.ultraThinMaterial)
                .opacity(0.7)
                .overlay(RoundedRectangle(cornerRadius: 24, style: .continuous)
                    .strokeBorder(.white.opacity(scheme == .dark ? 0.08 : 0.5), lineWidth: 1))
        }
        .padding(.horizontal, 16)
    }
}
