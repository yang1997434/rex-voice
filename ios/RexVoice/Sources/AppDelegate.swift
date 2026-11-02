import UIKit
import UserNotifications

/// APNs token, foreground banners and the「Claude 在等你」push actions. The Mac asks the relay to push (e.g. Claude
/// is stuck); a push with category `claude-ask` carries the pane's `block`, and its actions answer it without a call.
/// Tapping the push itself only opens the app.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    /// Set once the scene is up; a token that arrived earlier is handed over then.
    var controller: CallController? {
        didSet { if let token { controller?.setPushToken(token) } }
    }
    private var token: String?

    static let askCategory = "claude-ask"

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        // Fixed actions (docs/protocol.md section 2). All need an unlocked phone: the pairing key is only readable then,
        // and a locked phone must not approve a plan.
        let auth: UNNotificationActionOptions = [.authenticationRequired]
        let actions = (1...3).map { UNNotificationAction(identifier: "pick\($0)", title: "选 \($0)", options: auth) } + [
            UNTextInputNotificationAction(identifier: "reply", title: "输入回复…", options: auth,
                                          textInputButtonTitle: "发送", textInputPlaceholder: "回复 Claude"),
            UNNotificationAction(identifier: "esc", title: "取消（Esc）", options: auth),
        ]
        center.setNotificationCategories([UNNotificationCategory(identifier: Self.askCategory, actions: actions, intentIdentifiers: [])])
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
        token = hex
        controller?.setPushToken(hex)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        controller?.notice = "推送注册失败：\(error.localizedDescription)"
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter,
                                            willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard let block = response.notification.request.content.userInfo["block"] as? String else { return }
        var keys: [String]?, text: String?
        switch response.actionIdentifier {
        case "pick1", "pick2", "pick3": keys = [String(response.actionIdentifier.suffix(1))]
        case "esc": keys = ["escape"]
        case "reply":
            text = (response as? UNTextInputNotificationResponse)?.userText.trimmingCharacters(in: .whitespacesAndNewlines)
            if text?.isEmpty ?? true { return }
        default: return // the push itself: opening the app is all it does
        }
        await answer(block: block, keys: keys, text: text)
    }

    /// The app may have been launched in the background just for this, with no scene: the controller made in
    /// `RexVoiceApp.init` is reached through `CallController.shared`. A background task keeps it alive until the
    /// Mac confirms or `answer` gives up (≤ 22 s); a failure is told with a local notification.
    private func answer(block: String, keys: [String]?, text: String?) async {
        guard let c = controller ?? CallController.shared else { return }
        let app = UIApplication.shared
        let task = app.beginBackgroundTask(withName: "answer", expirationHandler: nil)
        defer { if task != .invalid { app.endBackgroundTask(task) } }
        guard let failure = await c.answer(block: block, keys: keys, text: text) else { return }
        let content = UNMutableNotificationContent()
        content.title = "回答没发出去"
        content.body = failure
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "answer-failed-\(block)",
                                                                                 content: content, trigger: nil))
    }
}
