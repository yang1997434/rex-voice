import SwiftUI

@main
struct RexVoiceApp: App {
    @State private var controller: CallController

    init() {
        let c = CallController()
        #if DEBUG
        if let screen = PreviewScreen.current { c.applyPreview(screen) }
        #endif
        _controller = State(initialValue: c)
    }

    var body: some Scene {
        WindowGroup {
            root.environment(controller)
        }
    }

    @ViewBuilder private var root: some View {
        #if DEBUG
        if let variant = DesignVariant.current {
            DesignRoot(variant: variant, initialSheet: PreviewScreen.current.flatMap { DesignSheet(rawValue: $0.rawValue) })
        } else {
            ContentView()
        }
        #else
        ContentView()
        #endif
    }
}
