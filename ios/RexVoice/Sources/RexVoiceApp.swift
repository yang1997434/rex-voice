import SwiftUI

@main
struct RexVoiceApp: App {
    @State private var controller = CallController()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environment(controller)
        }
    }
}
