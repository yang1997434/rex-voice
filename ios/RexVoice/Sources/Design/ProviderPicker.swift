import SwiftUI

// Provider logos; the rail picks from them with `ProviderWheel` (2026-11-02, round 4).

/// The official mark (lobehub/icons-static-svg, MIT; `Assets.xcassets/Logos`, template vectors) in white on the
/// brand-colored disc.
struct ProviderLogo: View {
    let provider: VoiceProvider
    var size: CGFloat = 36

    var body: some View {
        let g = size * 0.6
        ZStack {
            Circle().fill(provider.brand)
            Circle().strokeBorder(.white.opacity(0.18), lineWidth: 0.5)
            Image("logo-\(provider.rawValue)")
                .resizable()
                .renderingMode(.template)
                .scaledToFit()
                .frame(width: g, height: g)
        }
        .foregroundStyle(.white)
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}
