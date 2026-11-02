import SwiftUI

/// Design J's call dialog: bubbles with me on the right and Rex on the left,
/// attachments as cards inside Rex's bubbles, in arrival order. Follows the newest message unless the user has
/// scrolled up; then a「回到最新」 capsule brings it back.
struct DialogList: View {
    let lines: [CallController.Line]
    let attachments: [CallController.Attachment]
    let accent: Color
    let onImage: (CallController.Attachment) -> Void
    @Environment(\.colorScheme) private var scheme
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true

    private enum Item: Identifiable {
        case line(CallController.Line), attachment(CallController.Attachment)
        var id: UUID {
            switch self {
            case let .line(l): l.id
            case let .attachment(a): a.id
            }
        }
        var time: Date {
            switch self {
            case let .line(l): l.time
            case let .attachment(a): a.time
            }
        }
    }

    var body: some View {
        // index breaks ties so equal timestamps keep their order (sort is not stable)
        let items = (lines.map(Item.line) + attachments.map(Item.attachment)).enumerated()
            .sorted { ($0.element.time, $0.offset) < ($1.element.time, $1.offset) }.map(\.element)
        ScrollView {
            LazyVStack(spacing: 8) {
                if items.isEmpty {
                    Text("说话就会出现在这里").font(.subheadline).foregroundStyle(.secondary).padding(.top, 24)
                }
                ForEach(items) { item in
                    switch item {
                    case let .line(l): bubble(l)
                    case let .attachment(a): attachmentBubble(a)
                    }
                }
            }
            .padding(.horizontal, 16) // room for bubble shadows inside the clip; undone outside below
            .padding(.vertical, 12)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.top, for: .alignment) // a short dialog starts under the card, not at the bottom
        .defaultScrollAnchor(.bottom, for: .initialOffset)
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: Bool.self) { g in
            g.visibleRect.maxY >= g.contentSize.height - 24
        } action: { _, now in atBottom = now }
        .onChange(of: items.count) {
            if atBottom { withAnimation(.smooth) { position.scrollTo(edge: .bottom) } }
        }
        .overlay(alignment: .bottom) {
            if !atBottom {
                Button { withAnimation(.smooth) { position.scrollTo(edge: .bottom) } } label: {
                    Label("回到最新", systemImage: "arrow.down")
                        .font(.footnote.weight(.semibold))
                        .padding(.horizontal, 14)
                        .frame(minHeight: 34)
                        .glassEffect(.regular.interactive(), in: .capsule)
                }
                .buttonStyle(PressScale())
                .foregroundStyle(.primary)
                .padding(.bottom, 8)
                .transition(.opacity.combined(with: .scale(scale: 0.9)))
            }
        }
        .animation(.smooth(duration: 0.25), value: atBottom)
        .mask { // soft top edge where bubbles scroll away
            VStack(spacing: 0) {
                LinearGradient(colors: [.clear, .black], startPoint: .top, endPoint: .bottom).frame(height: 14)
                Color.black
            }
        }
        .padding(.horizontal, -12)
    }

    /// Rex's side: a plain light surface with a hairline and a small shadow (the big cards' shadow is too heavy here).
    private var rexSurface: some View {
        let dark = scheme == .dark
        return bubbleShape.fill(dark ? Color(white: 0.13) : .white)
            .overlay(bubbleShape.strokeBorder(.white.opacity(dark ? 0.1 : 0), lineWidth: 1))
            .shadow(color: .black.opacity(dark ? 0.3 : 0.06), radius: 4, y: 2)
    }

    private var bubbleShape: RoundedRectangle { RoundedRectangle(cornerRadius: 12, style: .continuous) }

    private func bubble(_ l: CallController.Line) -> some View {
        let dark = scheme == .dark
        return HStack(spacing: 0) {
            if l.fromUser { Spacer(minLength: 56) }
            Text(l.text)
                .font(.subheadline)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background {
                    if l.fromUser {
                        bubbleShape.fill(accent.opacity(dark ? 0.26 : 0.2))
                            .overlay(bubbleShape.strokeBorder(accent.opacity(dark ? 0.3 : 0.25), lineWidth: 1))
                    } else {
                        rexSurface
                    }
                }
            if !l.fromUser { Spacer(minLength: 56) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(l.fromUser ? "我" : "Rex")：\(l.text)")
        .transition(.opacity.combined(with: .move(edge: .bottom)))
    }

    /// Images open full screen, links open in Safari (web links only, checked where they arrive).
    private func attachmentBubble(_ a: CallController.Attachment) -> some View {
        HStack(spacing: 0) {
            Group {
                switch a.kind {
                case let .image(image, name):
                    Button { onImage(a) } label: {
                        Image(uiImage: image).resizable().scaledToFill()
                            .frame(width: 200, height: 125)
                            .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                    }
                    .buttonStyle(PressScale())
                    .accessibilityLabel("Rex 发来图片 \(name)")
                case let .link(url, title):
                    Link(destination: url) {
                        HStack(spacing: 10) {
                            Image(systemName: "link")
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(accent)
                                .frame(width: 32, height: 32)
                                .background(Circle().fill(accent.opacity(0.15)))
                            VStack(alignment: .leading, spacing: 2) {
                                Text(title).font(.subheadline.weight(.medium)).lineLimit(2)
                                Text(url.host() ?? url.absoluteString).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        .frame(maxWidth: 240, alignment: .leading)
                    }
                    .foregroundStyle(.primary)
                    .accessibilityLabel("Rex 发来链接 \(title)")
                }
            }
            .padding(6)
            .padding(.trailing, a.isLink ? 8 : 0)
            .background(rexSurface)
            Spacer(minLength: 56)
        }
        .transition(.opacity.combined(with: .move(edge: .bottom)))
    }
}

private extension CallController.Attachment {
    var isLink: Bool { if case .link = kind { true } else { false } }
}
