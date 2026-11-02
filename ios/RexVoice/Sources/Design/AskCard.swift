import SwiftUI

/// Questions Claude panes on the Mac are waiting on (`CallController.asks`), one card each, swiped left / right when
/// there are several. Shown idle and in a call, between the persona and the dialog. An option sends its number key,
/// the field sends text (the Mac's send_to_pane gate applies), Esc cancels the box.
struct AskCards: View {
    @Environment(CallController.self) private var controller
    let accent: Color

    var body: some View {
        let asks = controller.asks
        if !asks.isEmpty {
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: 12) {
                    ForEach(Array(asks.enumerated()), id: \.element.id) { i, ask in
                        AskCard(ask: ask, index: i, count: asks.count, accent: accent)
                            .containerRelativeFrame(.horizontal)
                            // Unclipped so the card shadow draws in full; a card sliding out fades instead, so the
                            // next one never shows over the rail.
                            .scrollTransition(.interactive) { card, phase in card.opacity(1 - abs(phase.value)) }
                    }
                }
                .scrollTargetLayout()
            }
            .scrollTargetBehavior(.viewAligned)
            .scrollIndicators(.hidden)
            .scrollClipDisabled()
            .transition(.opacity.combined(with: .move(edge: .top)))
        }
    }
}

private struct AskCard: View {
    @Environment(CallController.self) private var controller
    let ask: CallController.Ask
    let index: Int
    let count: Int
    let accent: Color
    @State private var expanded = false
    @State private var reply = ""
    /// Which control was pressed last, so the spinner shows on it while sending: "1"..."6", "text" or "esc".
    @State private var pressed: String?

    private var sending: Bool { ask.phase == .sending }
    private var fill: Color { .primary.opacity(0.06) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            Text(ask.msg.isEmpty ? "（没写问题）" : ask.msg)
                .font(.subheadline)
                .lineLimit(expanded ? nil : 3)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
                .onTapGesture { expanded.toggle() }
                .accessibilityAddTraits(.isButton)
                .accessibilityHint(expanded ? "收起" : "展开全文")
            if !ask.options.isEmpty {
                VStack(spacing: 8) {
                    ForEach(Array(ask.options.enumerated()), id: \.offset) { i, option in optionButton(i + 1, option) }
                }
            }
            if case let .failed(why) = ask.phase {
                Label("没发出去：\(why)", systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
            replyRow
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true) // natural height: the persona card above is the one that gives way
        .background(CardSurface(radius: 24).opacity(0.92))
        .disabled(sending)
    }

    private var header: some View {
        HStack(spacing: 8) {
            Image(systemName: "questionmark.bubble.fill").foregroundStyle(accent)
            Text("\(ask.project) 的 Claude 在问").font(.subheadline.weight(.semibold)).lineLimit(1)
            Spacer(minLength: 0)
            if ask.phase == .answered {
                Label("已回答", systemImage: "checkmark.circle.fill")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.green)
                    .transition(.opacity)
            } else if count > 1 {
                Text("\(index + 1)/\(count)").font(.footnote.monospacedDigit()).foregroundStyle(.secondary)
            }
        }
        .animation(.easeOut(duration: 0.15), value: ask.phase)
    }

    private func optionButton(_ n: Int, _ option: String) -> some View {
        Button { send(tag: "\(n)", keys: ["\(n)"]) } label: {
            HStack(spacing: 10) {
                Text("\(n)")
                    .font(.footnote.weight(.bold).monospacedDigit())
                    .foregroundStyle(accent)
                    .frame(width: 24, height: 24)
                    .background(Circle().fill(accent.opacity(0.15)))
                Text(option).font(.subheadline).lineLimit(2).multilineTextAlignment(.leading)
                Spacer(minLength: 0)
                if sending, pressed == "\(n)" { ProgressView().controlSize(.small) }
            }
            .padding(.horizontal, 12)
            .frame(minHeight: 44)
            .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(fill))
            .contentShape(.rect)
        }
        .buttonStyle(PressScale())
        .foregroundStyle(.primary)
        .accessibilityLabel("选 \(n)：\(option)")
    }

    private var replyRow: some View {
        let empty = reply.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return HStack(spacing: 8) {
            TextField("输入回复…", text: $reply)
                .font(.subheadline)
                .submitLabel(.send)
                .onSubmit(sendText)
                .padding(.horizontal, 14)
                .frame(minHeight: 44)
                .background(Capsule().fill(fill))
            Button(action: sendText) {
                ZStack {
                    if sending, pressed == "text" { ProgressView().tint(.white) } else {
                        Image(systemName: "arrow.up").font(.subheadline.weight(.bold))
                    }
                }
                .frame(width: 44, height: 44)
                .background(Circle().fill(accent))
                .foregroundStyle(.white)
            }
            .buttonStyle(PressScale())
            .disabled(empty)
            .opacity(empty ? 0.45 : 1)
            .accessibilityLabel("发送回复")
            Button { send(tag: "esc", keys: ["escape"]) } label: {
                ZStack {
                    if sending, pressed == "esc" { ProgressView().controlSize(.small) } else { Text("Esc") }
                }
                .font(.footnote.weight(.semibold))
                .frame(minWidth: 48, minHeight: 44)
                .background(Capsule().fill(fill))
            }
            .buttonStyle(PressScale())
            .foregroundStyle(.secondary)
            .accessibilityLabel("取消提问（Esc）")
        }
    }

    private func sendText() {
        let text = reply.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        send(tag: "text", text: text)
    }

    private func send(tag: String, keys: [String]? = nil, text: String? = nil) {
        pressed = tag
        Task {
            if await controller.answer(block: ask.block, keys: keys, text: text) == nil, text != nil { reply = "" }
        }
    }
}
