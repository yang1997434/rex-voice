import Foundation
import RexVoiceCore

/// One phone connection to the relay (docs/protocol.md sections 2-3): salt exchange, per-connection
/// ciphers, keepalive and reconnect with 1 s -> 30 s backoff. Events come out of `events`.
actor RelayClient {
    enum Event: Sendable {
        case connecting
        case macOffline
        /// Keys derived and `hello` sent; control and audio frames can flow.
        case ready
        case control(ControlMessage)
        case audio(Data)
        case failed(String)
    }

    nonisolated let events: AsyncStream<Event>
    private let emit: AsyncStream<Event>.Continuation
    private let url: URL
    private let userAgent: String
    private let hello: ControlMessage
    private var hs: Handshake
    private var ws: URLSessionWebSocketTask?
    private var runner: Task<Void, Never>?
    private var watchdog: Task<Void, Never>?
    private let session = URLSession(configuration: .default)

    init(pairing: Pairing, userAgent: String, hello: ControlMessage) {
        var c = URLComponents(url: pairing.relay, resolvingAgainstBaseURL: false)!
        c.queryItems = (c.queryItems ?? []) + [
            URLQueryItem(name: "role", value: "phone"),
            URLQueryItem(name: "id", value: Self.base64URL(pairing.id)),
        ]
        url = c.url!
        self.userAgent = userAgent
        self.hello = hello
        hs = Handshake(key: pairing.key)
        (events, emit) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(256))
    }

    func start() {
        guard runner == nil else { return }
        runner = Task { await self.run() }
    }

    /// Closes the connection for good and finishes `events`.
    func stop() {
        runner?.cancel()
        runner = nil
        ws?.cancel(with: .goingAway, reason: nil)
        emit.finish()
    }

    /// Returns false when not ready (frame dropped).
    @discardableResult
    func send(_ message: ControlMessage) -> Bool {
        guard let json = try? JSONEncoder().encode(message) else { return false }
        return seal(.control, json)
    }

    @discardableResult
    func sendAudio(_ pcm: Data) -> Bool { seal(.audio, pcm) }

    // MARK: - connection loop

    private func run() async {
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            emit.yield(.connecting)
            if await connectOnce() { delay = .seconds(1) }
            guard !Task.isCancelled else { break }
            try? await Task.sleep(for: delay)
            delay = min(delay * 2, .seconds(30))
        }
    }

    /// Runs one WebSocket until it fails. Returns whether anything was received (resets the backoff).
    private func connectOnce() async -> Bool {
        var req = URLRequest(url: url)
        req.setValue(userAgent, forHTTPHeaderField: "User-Agent")
        let task = session.webSocketTask(with: req)
        ws = task
        task.resume()
        write(hs.connected(), on: task)
        let keepalive = Task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(30))
                self.write(KeepaliveFrame.data, on: task)
            }
        }
        defer {
            keepalive.cancel()
            watchdog?.cancel()
            task.cancel(with: .goingAway, reason: nil)
            if ws === task { ws = nil }
            _ = hs.peer(present: false)
        }
        var received = false
        do {
            while !Task.isCancelled {
                let message = try await task.receive()
                received = true
                handle(message, on: task)
            }
        } catch {
            if !Task.isCancelled { emit.yield(.failed("连接断开：\(error.localizedDescription)")) }
        }
        return received
    }

    private func handle(_ message: URLSessionWebSocketTask.Message, on task: URLSessionWebSocketTask) {
        switch message {
        case let .string(text):
            onRelayText(text, on: task)
        case let .data(frame):
            onFrame(frame, on: task)
        @unknown default:
            break
        }
    }

    private struct PeerNotice: Decodable {
        let t: String
        let present: Bool
    }

    private func onRelayText(_ text: String, on task: URLSessionWebSocketTask) {
        guard let n = try? JSONDecoder().decode(PeerNotice.self, from: Data(text.utf8)), n.t == "peer" else { return }
        guard n.present else {
            _ = hs.peer(present: false)
            watchdog?.cancel()
            emit.yield(.macOffline)
            return
        }
        if let salt = hs.peer(present: true) { write(salt, on: task) }
        emit.yield(.connecting)
        // A lost or out-of-order salt would otherwise hang here forever; a new connection starts clean.
        watchdog?.cancel()
        watchdog = Task {
            try? await Task.sleep(for: .seconds(10))
            guard !Task.isCancelled else { return }
            self.handshakeTimedOut(task)
        }
    }

    private func handshakeTimedOut(_ task: URLSessionWebSocketTask) {
        guard ws === task, !hs.ready else { return }
        emit.yield(.failed("等 Mac 握手超时，重连"))
        task.cancel(with: .goingAway, reason: nil)
    }

    private func onFrame(_ frame: Data, on task: URLSessionWebSocketTask) {
        guard let first = frame.first, let type = FrameType(rawValue: first) else { return }
        switch type {
        case .keepalive:
            return
        case .salt:
            guard let salt = SaltFrame.parse(frame), hs.mac(salt: salt) else { return }
            watchdog?.cancel()
            send(hello)
            emit.yield(.ready)
        case .audio, .control:
            guard let rx = hs.rx else { return } // before the salt exchange: drop
            let opened: (type: FrameType, seq: UInt32, plain: Data)
            do {
                opened = try rx.open(frame)
            } catch FrameCipherError.replay {
                emit.yield(.failed("收到重放帧，断开重连"))
                task.cancel(with: .goingAway, reason: nil)
                return
            } catch {
                return // failed authentication: drop, like the Mac side
            }
            if opened.type == .audio {
                emit.yield(.audio(opened.plain))
            } else if let m = try? JSONDecoder().decode(ControlMessage.self, from: opened.plain) {
                emit.yield(.control(m))
            }
        }
    }

    // MARK: - writing

    /// Seal and hand to URLSession in one synchronous step, so seq order equals wire order.
    private func seal(_ type: FrameType, _ plain: Data) -> Bool {
        guard let task = ws, let tx = hs.tx else { return false }
        do {
            write(try tx.seal(type: type, plain), on: task)
            return true
        } catch {
            emit.yield(.failed("发送序号用尽，重连"))
            task.cancel(with: .goingAway, reason: nil)
            return false
        }
    }

    private func write(_ frame: Data, on task: URLSessionWebSocketTask) {
        task.send(.data(frame)) { error in
            if error != nil { task.cancel(with: .goingAway, reason: nil) } // receive() then fails and we reconnect
        }
    }

    static func base64URL(_ d: Data) -> String {
        d.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
