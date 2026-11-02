// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "RexVoiceCore",
    platforms: [.iOS(.v18), .macOS(.v15)],
    products: [.library(name: "RexVoiceCore", targets: ["RexVoiceCore"])],
    targets: [
        .target(name: "RexVoiceCore"),
        .testTarget(name: "RexVoiceCoreTests", dependencies: ["RexVoiceCore"]),
    ],
    swiftLanguageModes: [.v6]
)
